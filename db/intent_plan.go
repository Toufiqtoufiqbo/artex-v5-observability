package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Phase IX.2.2 — planned intent creation, duplicate suppression, dependencies.
//
// This file is the single application-level enforcement point for intent
// identity, mirroring the IX.2.1 decision to enforce finding identity inside
// RecordFindingTx. Both the planner's add_intent tool and every other caller of
// ExplorationStore.AddIntent funnel through AddIntentPlanned.
//
// Concurrency: check-and-insert runs inside one transaction guarded by a
// transaction-scoped advisory lock namespaced per exploration. Unrelated
// assessments never serialize against each other.

const (
	// MaxIntentBatch bounds one add_intent call. The planner prompt asks for at
	// most 4; the hard cap is an administrative limit the model cannot raise.
	MaxIntentBatch = 8
	// MaxIntentDependencies bounds depends_on per intent.
	MaxIntentDependencies = 8
	// MaxIntentsPerExploration is the administrative ceiling on total intent
	// rows (any state) in one task. It is a code-level constant, not an LLM
	// budget: model output can never raise it. Reaching it rejects new intents
	// with a structured error instead of allowing unbounded planning.
	MaxIntentsPerExploration = 500
)

var (
	// ErrIntentInvalid marks a rejected proposal (malformed or unsupported).
	ErrIntentInvalid = errors.New("intent: invalid proposal")
	// ErrIntentBudgetExhausted marks the administrative intent ceiling.
	ErrIntentBudgetExhausted = errors.New("intent: exploration intent budget exhausted")
	// ErrIntentDependencyCycle marks an existing dependency graph that already
	// contains a cycle; new dependent work is refused until it is repaired.
	ErrIntentDependencyCycle = errors.New("intent: dependency graph contains a cycle")
)

// Intent plan outcomes. They are additive diagnostics; callers that only need
// the id can ignore them.
const (
	IntentOutcomeNew       = "new"       // no prior proposal in scope
	IntentOutcomeDuplicate = "duplicate" // active prior proposal reused
	IntentOutcomeRepeat    = "repeat"    // prior proposal completed; new attempt linked
	IntentOutcomeRetry     = "retry"     // prior proposal failed/stopped; retry linked
	IntentOutcomeRetest    = "retest"    // explicit retest_of requested
)

// IntentPlanRequest is one validated intent proposal.
type IntentPlanRequest struct {
	Summary   string
	Operation string
	Resource  string
	Priority  int
	Anchors   []int64
	Parents   []int64
	DependsOn []int64
	RetestOf  int64
	Origin    string
}

// IntentPlanResult reports what the store did with a proposal.
type IntentPlanResult struct {
	ID       int64  `json:"id"`
	Outcome  string `json:"outcome"`
	ReusedID int64  `json:"reused_id,omitempty"`
}

// IntentTerminalFailure reports whether an intent state means the work did not
// complete successfully and will not produce its expected output.
func IntentTerminalFailure(state string) bool {
	switch state {
	case "blocked", "exhausted", "stopped", "deleted":
		return true
	}
	return false
}

// AddIntent is the compatibility entry point: it keeps the historical signature
// but now enforces the intent identity contract. Duplicate active proposals
// resolve to the existing intent id.
func (s *ExplorationStore) AddIntent(payload map[string]any, priority int, anchors []int64, origin string) (int64, error) {
	req := IntentPlanRequest{Priority: priority, Anchors: anchors, Origin: origin}
	if v, ok := payload["summary"].(string); ok {
		req.Summary = v
	}
	if v, ok := payload["operation"].(string); ok {
		req.Operation = v
	}
	if v, ok := payload["resource"].(string); ok {
		req.Resource = v
	}
	res, err := s.AddIntentPlanned(req)
	if err != nil {
		return 0, err
	}
	return res.ID, nil
}

// AddIntentPlanned validates, deduplicates and inserts one intent atomically.
func (s *ExplorationStore) AddIntentPlanned(req IntentPlanRequest) (IntentPlanResult, error) {
	if req.Origin == "" {
		req.Origin = "planner"
	}
	deps, err := normalizeIntentIDs(req.DependsOn, "depends_on")
	if err != nil {
		recordIntentReject("depends_on")
		return IntentPlanResult{}, err
	}
	if len(deps) > MaxIntentDependencies {
		recordIntentReject("depends_on_limit")
		return IntentPlanResult{}, fmt.Errorf("%w: depends_on has %d entries (max %d)", ErrIntentInvalid, len(deps), MaxIntentDependencies)
	}
	parents := uniqueIntentIDs(req.Parents)
	anchors := uniqueIntentIDs(req.Anchors)
	if req.RetestOf < 0 {
		recordIntentReject("retest_of")
		return IntentPlanResult{}, fmt.Errorf("%w: retest_of must be a positive intent id", ErrIntentInvalid)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return IntentPlanResult{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Per-exploration transaction-scoped lock: makes check-and-insert atomic
	// without serialising independent tasks.
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('artex.intent.' || ($1::bigint)::text, 0))`, s.expID); err != nil {
		return IntentPlanResult{}, err
	}

	// Administrative ceiling. Counts every intent row, including terminal ones,
	// so repeated retests cannot grow the graph without bound.
	var total int
	if err := tx.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='intent'`, s.expID).Scan(&total); err != nil {
		return IntentPlanResult{}, err
	}
	if total >= MaxIntentsPerExploration {
		recordIntentReject("budget")
		return IntentPlanResult{}, fmt.Errorf("%w: %d intents already exist (max %d)", ErrIntentBudgetExhausted, total, MaxIntentsPerExploration)
	}

	// Dependency validation: every prerequisite must exist, be an intent in the
	// same exploration, and the existing graph must stay acyclic.
	for _, dep := range deps {
		var kind string
		err := tx.QueryRow(`SELECT kind FROM exploration_nodes WHERE id=$1 AND exploration_id=$2`, dep, s.expID).Scan(&kind)
		if errors.Is(err, sql.ErrNoRows) {
			recordIntentReject("depends_on_missing")
			return IntentPlanResult{}, fmt.Errorf("%w: depends_on %d does not exist in this exploration", ErrIntentInvalid, dep)
		}
		if err != nil {
			return IntentPlanResult{}, err
		}
		if kind != KindIntent {
			recordIntentReject("depends_on_kind")
			return IntentPlanResult{}, fmt.Errorf("%w: depends_on %d is a %q node, not an intent", ErrIntentInvalid, dep, kind)
		}
	}
	if req.RetestOf > 0 {
		var kind string
		err := tx.QueryRow(`SELECT kind FROM exploration_nodes WHERE id=$1 AND exploration_id=$2`, req.RetestOf, s.expID).Scan(&kind)
		if errors.Is(err, sql.ErrNoRows) {
			recordIntentReject("retest_of_missing")
			return IntentPlanResult{}, fmt.Errorf("%w: retest_of %d does not exist in this exploration", ErrIntentInvalid, req.RetestOf)
		}
		if err != nil {
			return IntentPlanResult{}, err
		}
		if kind != KindIntent {
			recordIntentReject("retest_of_kind")
			return IntentPlanResult{}, fmt.Errorf("%w: retest_of %d is a %q node, not an intent", ErrIntentInvalid, req.RetestOf, kind)
		}
	}
	if len(deps) > 0 {
		if err := intentGraphAcyclic(tx, s.expID); err != nil {
			recordIntentReject("dependency_cycle")
			return IntentPlanResult{}, err
		}
	}

	fingerprint := IntentFingerprint(IntentIdentityInput{
		ExplorationID: s.expID,
		Summary:       req.Summary,
		Operation:     req.Operation,
		Resource:      req.Resource,
		AssetIDs:      anchors,
	})

	outcome := IntentOutcomeNew
	var priorID int64
	if req.RetestOf == 0 {
		var priorState string
		err := tx.QueryRow(`
SELECT id, state FROM exploration_nodes
WHERE exploration_id=$1 AND kind='intent' AND payload->>'intent_fingerprint'=$2
ORDER BY (state IN ('open','running','paused')) DESC, id DESC
LIMIT 1`, s.expID, fingerprint).Scan(&priorID, &priorState)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// fresh work
		case err != nil:
			return IntentPlanResult{}, err
		case priorState == "open" || priorState == "running" || priorState == "paused":
			// Active duplicate: reuse it. Parents cited by this proposal are
			// attached to the existing node so newly cited evidence is not lost.
			for _, p := range parents {
				if _, lerr := tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
					s.expID, p, RelDerivedFrom, priorID); lerr != nil {
					return IntentPlanResult{}, lerr
				}
			}
			if err := tx.Commit(); err != nil {
				return IntentPlanResult{}, err
			}
			recordIntentPlan(IntentOutcomeDuplicate)
			return IntentPlanResult{ID: priorID, Outcome: IntentOutcomeDuplicate, ReusedID: priorID}, nil
		case priorState == "done":
			outcome = IntentOutcomeRepeat
		default:
			// blocked / exhausted / stopped / deleted
			outcome = IntentOutcomeRetry
		}
	}

	payload := map[string]any{"summary": req.Summary, "intent_fingerprint": fingerprint}
	if strings.TrimSpace(req.Operation) != "" {
		payload["operation"] = req.Operation
	}
	if strings.TrimSpace(req.Resource) != "" {
		payload["resource"] = req.Resource
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return IntentPlanResult{}, err
	}
	priority := req.Priority
	if priority == 0 {
		priority = 5
	}
	var id int64
	if err := tx.QueryRow(`
INSERT INTO exploration_nodes(exploration_id, kind, payload, priority, state, origin)
VALUES ($1, 'intent', $2, $3, 'open', $4) RETURNING id`,
		s.expID, string(raw), priority, req.Origin).Scan(&id); err != nil {
		return IntentPlanResult{}, err
	}
	for _, a := range anchors {
		if _, err := tx.Exec(`INSERT INTO exploration_anchors(node_id, asset_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, a); err != nil {
			return IntentPlanResult{}, err
		}
	}
	// Provenance: parents → new intent. A top-level intent still traces to the
	// task root so every intent derives from a fact node.
	for _, p := range parents {
		if _, err := tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			s.expID, p, RelDerivedFrom, id); err != nil {
			return IntentPlanResult{}, err
		}
	}
	if len(parents) == 0 {
		var origin int64
		if err := tx.QueryRow(`SELECT id FROM exploration_nodes WHERE exploration_id=$1 AND kind='fact' AND state='origin' ORDER BY id LIMIT 1`, s.expID).Scan(&origin); err == nil && origin > 0 {
			if _, lerr := tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
				s.expID, origin, RelDerivedFrom, id); lerr != nil {
				return IntentPlanResult{}, lerr
			}
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return IntentPlanResult{}, err
		}
	}
	// Execution dependencies: dependent → prerequisite.
	for _, dep := range deps {
		if _, err := tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			s.expID, id, RelDependsOn, dep); err != nil {
			return IntentPlanResult{}, err
		}
	}
	switch {
	case req.RetestOf > 0:
		outcome = IntentOutcomeRetest
		if _, err := tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			s.expID, id, RelRetestOf, req.RetestOf); err != nil {
			return IntentPlanResult{}, err
		}
	case priorID > 0 && outcome == IntentOutcomeRepeat:
		if _, err := tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			s.expID, id, RelRetestOf, priorID); err != nil {
			return IntentPlanResult{}, err
		}
	case priorID > 0 && outcome == IntentOutcomeRetry:
		if _, err := tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			s.expID, id, RelRetryOf, priorID); err != nil {
			return IntentPlanResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return IntentPlanResult{}, err
	}
	recordIntentPlan(outcome)
	return IntentPlanResult{ID: id, Outcome: outcome, ReusedID: priorID}, nil
}

// BlockDependents marks every open intent that transitively depends on intentID
// as blocked, recording why. It is called when a prerequisite settles without
// completing, so dependents never wait forever on work that cannot produce
// their input. Returns the number of intents blocked.
func (s *ExplorationStore) BlockDependents(intentID int64, reason string) (int64, error) {
	if intentID <= 0 {
		return 0, nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = fmt.Sprintf("prerequisite #%d failed", intentID)
	}
	res, err := s.db.Exec(`
WITH RECURSIVE dep(id) AS (
    SELECT src_id FROM exploration_edges
    WHERE exploration_id=$1 AND rel=$2 AND dst_id=$3
  UNION
    SELECT e.src_id FROM exploration_edges e JOIN dep d ON e.dst_id=d.id
    WHERE e.exploration_id=$1 AND e.rel=$2
)
UPDATE exploration_nodes
SET state='blocked', blocked_reason=$4, content_version=content_version+1, completed_at=now()
WHERE exploration_id=$1 AND kind='intent' AND state='open' AND id IN (SELECT id FROM dep)`,
		s.expID, RelDependsOn, intentID, reason)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		recordIntentBlocked(n)
	}
	return n, nil
}

// ReopenDependentsOf reopens intents that were blocked specifically because
// intentID failed, used when that prerequisite is reopened for another attempt.
func (s *ExplorationStore) ReopenDependentsOf(intentID int64) (int64, error) {
	if intentID <= 0 {
		return 0, nil
	}
	res, err := s.db.Exec(`
UPDATE exploration_nodes
SET state='open', blocked_reason=NULL, content_version=content_version+1, completed_at=NULL
WHERE exploration_id=$1 AND kind='intent' AND state='blocked' AND blocked_reason LIKE $2`,
		s.expID, fmt.Sprintf("prerequisite #%d %%", intentID))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// intentGraphAcyclic rejects an existing dependency graph that already contains
// a cycle. New nodes cannot create one (they have no incoming depends_on edge
// yet), so this is a guard against corrupted/imported graphs.
func intentGraphAcyclic(tx *sql.Tx, expID int64) error {
	rows, err := tx.Query(`SELECT src_id, dst_id FROM exploration_edges WHERE exploration_id=$1 AND rel=$2`, expID, RelDependsOn)
	if err != nil {
		return err
	}
	defer rows.Close()
	adj := map[int64][]int64{}
	for rows.Next() {
		var src, dst int64
		if err := rows.Scan(&src, &dst); err != nil {
			return err
		}
		adj[src] = append(adj[src], dst)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[int64]int{}
	var visit func(int64) bool
	visit = func(n int64) bool {
		switch color[n] {
		case grey:
			return true
		case black:
			return false
		}
		color[n] = grey
		for _, next := range adj[n] {
			if visit(next) {
				return true
			}
		}
		color[n] = black
		return false
	}
	keys := make([]int64, 0, len(adj))
	for k := range adj {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		if visit(k) {
			return fmt.Errorf("%w: cycle reachable from intent #%d", ErrIntentDependencyCycle, k)
		}
	}
	return nil
}

// normalizeIntentIDs rejects non-positive ids (a caller bug worth surfacing)
// and returns a de-duplicated, sorted copy.
func normalizeIntentIDs(ids []int64, field string) ([]int64, error) {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("%w: %s contains non-positive id %d", ErrIntentInvalid, field, id)
		}
		out = append(out, id)
	}
	return uniqueIntentIDs(out), nil
}

func uniqueIntentIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
