package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/server"
	"github.com/Autumn-27/norma/transcript"
)

// Phase IX.2.2 — planning benchmark runner.
//
// "after" mode drives the REAL Planner.Plan path (scripted provider, real
// add_intent tool, real ExplorationStore). "legacy" mode reproduces the
// pre-IX.2.2 write path verbatim: AddNode(kind='intent', state='open') — the
// exact body of the old ExplorationStore.AddIntent. Legacy mode therefore
// measures intent creation as it behaved before this phase and is labelled as
// such in the artifact; dependency cases are reported not_measured because the
// historical code had no dependency predicate to measure.
//
// Ground truth lives in planning_dataset.go and is never derived from output.

type PlanningCaseMetrics struct {
	CaseID                 string   `json:"case_id"`
	Category               string   `json:"category"`
	Status                 string   `json:"status"` // completed | not_measured | execution_error
	Submitted              int      `json:"submitted"`
	IntentsCreated         int      `json:"intents_created"`
	ExpectedLogicalIntents int      `json:"expected_logical_intents"`
	LogicalIntentDelta     int      `json:"logical_intent_delta"`
	DuplicatesCollapsed    int      `json:"duplicates_collapsed"`
	MissedCollapses        int      `json:"missed_collapses"`
	FalseMerges            int      `json:"false_merges"`
	GatingChecks           int      `json:"gating_checks"`
	GatingViolations       int      `json:"gating_violations"`
	PropagationChecks      int      `json:"propagation_checks"`
	PropagationViolations  int      `json:"propagation_violations"`
	BlockedDependents      int      `json:"blocked_dependents"`
	RetestEdges            int      `json:"retest_edges"`
	ExpectedRetestEdges    int      `json:"expected_retest_edges"`
	Pass                   bool     `json:"pass"`
	Errors                 []string `json:"errors,omitempty"`
	DurationMS             int64    `json:"duration_ms"`
}

type PlanningSuiteMetrics struct {
	SuiteID                     string  `json:"suite_id"`
	Mode                        string  `json:"mode"`
	Cases                       int     `json:"cases"`
	MeasuredCases               int     `json:"measured_cases"`
	Passed                      int     `json:"passed"`
	Submitted                   int     `json:"submitted"`
	IntentsCreated              int     `json:"intents_created"`
	ExpectedLogicalIntents      int     `json:"expected_logical_intents"`
	DuplicatesCollapsed         int     `json:"duplicates_collapsed"`
	MissedCollapses             int     `json:"missed_collapses"`
	FalseMerges                 int     `json:"false_merges"`
	GatingViolations            int     `json:"gating_violations"`
	PropagationViolations       int     `json:"propagation_violations"`
	BlockedDependents           int     `json:"blocked_dependents"`
	FalseMergeRate              float64 `json:"false_merge_rate"`
	DuplicateCollapseRate       float64 `json:"duplicate_collapse_rate"`
	IntentInflation             int     `json:"intent_inflation"` // created - expected
}

type PlanningArtifact struct {
	Manifest RunManifest          `json:"manifest"`
	Metrics  PlanningSuiteMetrics `json:"metrics"`
	Cases    []PlanningCaseMetrics `json:"cases"`
}

type PlanningRunner struct {
	mgr    *server.Manager
	pg     *db.DB
	dir    string
	legacy bool
}

func NewPlanningRunner(dir string, legacy bool) (*PlanningRunner, error) {
	mgr, err := server.NewManager(dir, "")
	if err != nil {
		return nil, fmt.Errorf("open manager: %w", err)
	}
	return &PlanningRunner{mgr: mgr, pg: mgr.PG(), dir: dir, legacy: legacy}, nil
}

func (r *PlanningRunner) Close() error { return r.mgr.Close() }

func (r *PlanningRunner) Mode() string {
	if r.legacy {
		return "legacy"
	}
	return "offline"
}

// flatProposals returns proposals in global round-major order.
func flatProposals(c *PlanningCase) []PlanningProposal {
	var out []PlanningProposal
	for _, round := range c.Rounds {
		out = append(out, round.Proposals...)
	}
	return out
}

func (r *PlanningRunner) RunCase(ctx context.Context, c *PlanningCase) PlanningCaseMetrics {
	res := PlanningCaseMetrics{CaseID: c.ID, Category: c.Category, ExpectedLogicalIntents: c.ExpectedLogicalIntents, ExpectedRetestEdges: c.ExpectedRetestEdges}
	start := time.Now()
	defer func() { res.DurationMS = time.Since(start).Milliseconds() }()

	st, err := r.mgr.CreateTaskWithOptions(fmt.Sprintf("bench-plan: %s", c.ID), fmt.Sprintf("规划基准用例 %s", c.ID), db.TaskCreateOptions{TimeoutSeconds: 120, PlanHeartbeatSeconds: 600})
	if err != nil {
		res.Status = "execution_error"
		res.Errors = append(res.Errors, err.Error())
		return res
	}
	defer func() {
		if taskIDInt, perr := strconv.ParseInt(st.ID, 10, 64); perr == nil {
			_ = r.mgr.PG().DeleteTask(taskIDInt)
		}
	}()
	ts := r.pg.Exploration(st.ExpID)
	taskIDInt, _ := strconv.ParseInt(st.ID, 10, 64)

	if r.legacy && c.Category == "planning_dependency" {
		res.Status = "not_measured"
		return res
	}

	flat := flatProposals(c)
	ids := make([]int64, len(flat))
	global := 0
	for _, round := range c.Rounds {
		var roundIDs []int64
		if r.legacy {
			for _, p := range round.Proposals {
				id, werr := r.legacyWrite(ts, p)
				if werr != nil {
					res.Status = "execution_error"
					res.Errors = append(res.Errors, werr.Error())
					return res
				}
				ids[global] = id
				roundIDs = append(roundIDs, id)
				global++
			}
		} else if round.Batch {
			payload := map[string]any{"intents": r.batchPayload(round.Proposals, ids, global)}
			if perr := r.planOnce(ctx, ts, taskIDInt, payload); perr != nil {
				res.Status = "execution_error"
				res.Errors = append(res.Errors, perr.Error())
				return res
			}
			for _, p := range round.Proposals {
				id, ferr := r.findIntent(ts, p)
				if ferr != nil {
					res.Status = "execution_error"
					res.Errors = append(res.Errors, ferr.Error())
					return res
				}
				ids[global] = id
				roundIDs = append(roundIDs, id)
				global++
			}
		} else {
			for _, p := range round.Proposals {
				payload := r.singlePayload(p, ids, global)
				if perr := r.planOnce(ctx, ts, taskIDInt, payload); perr != nil {
					res.Status = "execution_error"
					res.Errors = append(res.Errors, perr.Error())
					return res
				}
				id, ferr := r.findIntent(ts, p)
				if ferr != nil {
					res.Status = "execution_error"
					res.Errors = append(res.Errors, ferr.Error())
					return res
				}
				ids[global] = id
				roundIDs = append(roundIDs, id)
				global++
			}
		}

		if round.Complete {
			for _, id := range roundIDs {
				if serr := ts.SetIntentState(id, "done"); serr != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("complete intent %d: %v", id, serr))
				}
			}
		}
		if len(round.FailPrerequisites) > 0 {
			for _, idx := range round.FailPrerequisites {
				if idx < 0 || idx >= len(ids) || ids[idx] == 0 {
					continue
				}
				if serr := ts.SetIntentState(ids[idx], "blocked"); serr != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("fail prerequisite %d: %v", idx, serr))
					continue
				}
				if _, berr := ts.BlockDependents(ids[idx], fmt.Sprintf("prerequisite #%d ended blocked", ids[idx])); berr != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("propagate from %d: %v", ids[idx], berr))
				}
			}
		}
	}

	res.Submitted = len(flat)
	nodes, lerr := ts.ListByKind(db.KindIntent, 1000)
	if lerr != nil {
		res.Status = "execution_error"
		res.Errors = append(res.Errors, lerr.Error())
		return res
	}
	res.IntentsCreated = len(nodes)
	res.DuplicatesCollapsed = res.Submitted - res.IntentsCreated
	res.LogicalIntentDelta = res.IntentsCreated - res.ExpectedLogicalIntents
	res.RetestEdges = r.countEdges(ts, db.RelRetestOf)

	for _, group := range c.DuplicateGroups {
		if !allSame(ids, group) {
			res.MissedCollapses++
			res.Errors = append(res.Errors, fmt.Sprintf("duplicate group %v not collapsed: %v", group, pick(ids, group)))
		}
	}
	for _, group := range c.DistinctGroups {
		if !allDistinct(ids, group) {
			res.FalseMerges++
			res.Errors = append(res.Errors, fmt.Sprintf("distinct group %v merged: %v", group, pick(ids, group)))
		}
	}

	for _, pair := range c.GatedPairs {
		dep, pre := ids[pair.Dependent], ids[pair.Prerequisite]
		res.GatingChecks++
		if ok, gerr := ts.ClaimIntent(dep, "bench-gate"); gerr != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("gating claim: %v", gerr))
			res.GatingViolations++
			continue
		} else if ok {
			res.GatingViolations++
			res.Errors = append(res.Errors, fmt.Sprintf("dependent #%d claimable before prerequisite #%d done", dep, pre))
			_ = ts.SetIntentState(dep, "open")
			continue
		}
		if serr := ts.SetIntentState(pre, "done"); serr != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("prereq done: %v", serr))
			continue
		}
		if ok, gerr := ts.ClaimIntent(dep, "bench-gate"); gerr != nil || !ok {
			res.GatingViolations++
			res.Errors = append(res.Errors, fmt.Sprintf("dependent #%d not claimable after prerequisite #%d done (err=%v)", dep, pre, gerr))
		}
		_ = ts.SetIntentState(dep, "open")
	}

	blockedCount := 0
	for _, pair := range c.FailPairs {
		dep, pre := ids[pair.Dependent], ids[pair.Prerequisite]
		res.PropagationChecks++
		n, err := ts.BlockDependents(pre, fmt.Sprintf("prerequisite #%d ended blocked", pre))
		if err != nil {
			res.PropagationViolations++
			res.Errors = append(res.Errors, fmt.Sprintf("propagation: %v", err))
		} else {
			blockedCount += int(n)
			if n == 0 {
				res.PropagationViolations++
				res.Errors = append(res.Errors, fmt.Sprintf("prerequisite #%d blocked no dependents", pre))
			}
		}
		n2, gerr := ts.GetNode(dep)
		if gerr != nil || n2 == nil || n2.State != "blocked" {
			res.PropagationViolations++
			res.Errors = append(res.Errors, fmt.Sprintf("dependent #%d state=%v after prerequisite #%d failed", dep, stateOf(n2), pre))
		}
	}
	res.BlockedDependents = blockedCount
	if c.ExpectedBlockedDependents > 0 && blockedCount != c.ExpectedBlockedDependents {
		res.Errors = append(res.Errors, fmt.Sprintf("blocked dependents = %d, want %d", blockedCount, c.ExpectedBlockedDependents))
	}

	if res.RetestEdges < c.ExpectedRetestEdges {
		res.Errors = append(res.Errors, fmt.Sprintf("retest_of edges = %d, want >= %d", res.RetestEdges, c.ExpectedRetestEdges))
	}

	res.Status = "completed"
	res.Pass = len(res.Errors) == 0 &&
		res.LogicalIntentDelta == 0 &&
		res.MissedCollapses == 0 &&
		res.FalseMerges == 0 &&
		res.GatingViolations == 0 &&
		res.PropagationViolations == 0 &&
		res.RetestEdges >= c.ExpectedRetestEdges
	return res
}

// legacyWrite reproduces the pre-IX.2.2 AddIntent body exactly.
func (r *PlanningRunner) legacyWrite(ts *db.ExplorationStore, p PlanningProposal) (int64, error) {
	payload := map[string]any{"summary": p.Summary}
	if p.Operation != "" {
		payload["operation"] = p.Operation
	}
	if p.Resource != "" {
		payload["resource"] = p.Resource
	}
	priority := p.Priority
	if priority == 0 {
		priority = 5
	}
	return ts.AddNode("intent", payload, priority, "open", "planner", nil)
}

func (r *PlanningRunner) singlePayload(p PlanningProposal, ids []int64, idx int) map[string]any {
	out := map[string]any{"summary": p.Summary}
	if p.Operation != "" {
		out["operation"] = p.Operation
	}
	if p.Resource != "" {
		out["resource"] = p.Resource
	}
	if p.Priority != 0 {
		out["priority"] = p.Priority
	}
	if len(p.DependsOn) > 0 {
		out["depends_on"] = resolveIndices(ids, p.DependsOn)
	}
	if p.RetestOf > 0 && p.RetestOf < len(ids) {
		out["retest_of"] = ids[p.RetestOf]
	}
	return out
}

func (r *PlanningRunner) batchPayload(props []PlanningProposal, ids []int64, base int) []map[string]any {
	out := make([]map[string]any, 0, len(props))
	for i, p := range props {
		out = append(out, r.singlePayload(p, ids, base+i))
	}
	return out
}

func resolveIndices(ids []int64, idxs []int) []int64 {
	out := make([]int64, 0, len(idxs))
	for _, i := range idxs {
		if i >= 0 && i < len(ids) && ids[i] > 0 {
			out = append(out, ids[i])
		}
	}
	return out
}

// planOnce runs one real Planner.Plan round whose scripted model submits the
// given add_intent payload and then ends the turn.
func (r *PlanningRunner) planOnce(ctx context.Context, ts *db.ExplorationStore, taskID int64, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	prov := NewScriptedProvider([]ScriptResponse{
		{Lane: "planner", ToolUses: []ScriptToolUse{{Name: "add_intent", Input: raw}}},
		{Lane: "planner", Text: "已提交"},
	})
	tx := transcript.NewStore(filepath.Join(r.dir, "transcripts"))
	pl := agent.NewPlanner(prov, "mock", r.dir, tx, 0, 0)
	_, _, perr := pl.Plan(ctx, taskID, nil, ts, "按基准用例推进目标", nil, nil)
	return perr
}

// findIntent maps a proposal to its node id. Primary key is the intent
// identity fingerprint stored in the payload (the contract the planner must
// produce); the summary fallback covers legacy-path rows.
func (r *PlanningRunner) findIntent(ts *db.ExplorationStore, p PlanningProposal) (int64, error) {
	var id int64
	fp := db.IntentFingerprint(db.IntentIdentityInput{
		ExplorationID: ts.ID(), Summary: p.Summary, Operation: p.Operation, Resource: p.Resource,
	})
	err := r.pg.QueryRow(`SELECT id FROM exploration_nodes
WHERE exploration_id=$1 AND kind='intent' AND payload->>'intent_fingerprint'=$2
ORDER BY id DESC LIMIT 1`, ts.ID(), fp).Scan(&id)
	if err == nil {
		return id, nil
	}
	err = r.pg.QueryRow(`SELECT id FROM exploration_nodes
WHERE exploration_id=$1 AND kind='intent' AND payload->>'summary'=$2
ORDER BY id DESC LIMIT 1`, ts.ID(), p.Summary).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("locate intent %q: %w", p.Summary, err)
	}
	return id, nil
}

func (r *PlanningRunner) countEdges(ts *db.ExplorationStore, rel string) int {
	var n int
	if err := r.pg.QueryRow(`SELECT count(*) FROM exploration_edges WHERE exploration_id=$1 AND rel=$2`, ts.ID(), rel).Scan(&n); err != nil {
		return 0
	}
	return n
}

func allSame(ids []int64, group []int) bool {
	if len(group) < 2 {
		return true
	}
	first := ids[group[0]]
	for _, i := range group[1:] {
		if ids[i] != first {
			return false
		}
	}
	return true
}

func allDistinct(ids []int64, group []int) bool {
	seen := map[int64]bool{}
	for _, i := range group {
		if seen[ids[i]] {
			return false
		}
		seen[ids[i]] = true
	}
	return true
}

func pick(ids []int64, group []int) []int64 {
	out := make([]int64, 0, len(group))
	for _, i := range group {
		out = append(out, ids[i])
	}
	return out
}

func stateOf(n *db.Node) string {
	if n == nil {
		return "<nil>"
	}
	return n.State
}

// RunSuite executes every case and returns the aggregate artifact.
func (r *PlanningRunner) RunSuite(ctx context.Context, s *PlanningSuite, seed int64, started time.Time) PlanningArtifact {
	results := make([]PlanningCaseMetrics, 0, len(s.Cases))
	for _, c := range s.Cases {
		results = append(results, r.RunCase(ctx, c))
	}
	sm := PlanningSuiteMetrics{SuiteID: s.SuiteID, Mode: r.Mode(), Cases: len(results)}
	for _, res := range results {
		if res.Status != "completed" {
			continue
		}
		sm.MeasuredCases++
		if res.Pass {
			sm.Passed++
		}
		sm.Submitted += res.Submitted
		sm.IntentsCreated += res.IntentsCreated
		sm.ExpectedLogicalIntents += res.ExpectedLogicalIntents
		sm.DuplicatesCollapsed += res.DuplicatesCollapsed
		sm.MissedCollapses += res.MissedCollapses
		sm.FalseMerges += res.FalseMerges
		sm.GatingViolations += res.GatingViolations
		sm.PropagationViolations += res.PropagationViolations
		sm.BlockedDependents += res.BlockedDependents
	}
	sm.IntentInflation = sm.IntentsCreated - sm.ExpectedLogicalIntents
	if sm.ExpectedLogicalIntents > 0 {
		sm.FalseMergeRate = float64(sm.FalseMerges) / float64(sm.ExpectedLogicalIntents)
	}
	if sm.Submitted > 0 {
		sm.DuplicateCollapseRate = float64(sm.DuplicatesCollapsed) / float64(sm.Submitted)
	}

	_, configFP, redactions := EnvironmentSnapshot()
	worktreeFingerprint := gitOutput("status", "--short")
	execMode := "planning-runner"
	if r.legacy {
		execMode = "planning-legacy-write-path"
	}
	manifest := RunManifest{
		RunID:             NewRunID(r.Mode()),
		Mode:              r.Mode(),
		Seed:              seed,
		BenchmarkSuite:    s.SuiteID,
		SuiteVersion:      s.SuiteVersion,
		Cases:             len(results),
		Version:           server.BuildVersion,
		ExecutionMode:     execMode,
		StartedAt:         started,
		FinishedAt:        time.Now().UTC(),
		DurationSecs:      time.Since(started).Seconds(),
		ConfigFingerprint: configFP,
		Redactions:        redactions,
		GitHead:           gitOutput("rev-parse", "HEAD"),
		TreeFingerprint:   worktreeFingerprint,
		WorktreeEntries:   len(strings.Split(worktreeFingerprint, "\n")),
	}
	return PlanningArtifact{Manifest: manifest, Metrics: sm, Cases: results}
}

func (a PlanningArtifact) Save(path string) error {
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (a PlanningArtifact) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "planning suite=%s mode=%s cases=%d measured=%d passed=%d\n",
		a.Metrics.SuiteID, a.Metrics.Mode, a.Metrics.Cases, a.Metrics.MeasuredCases, a.Metrics.Passed)
	fmt.Fprintf(&b, "intents: submitted=%d created=%d expected_logical=%d inflation=%+d collapsed=%d missed_collapses=%d false_merges=%d\n",
		a.Metrics.Submitted, a.Metrics.IntentsCreated, a.Metrics.ExpectedLogicalIntents, a.Metrics.IntentInflation,
		a.Metrics.DuplicatesCollapsed, a.Metrics.MissedCollapses, a.Metrics.FalseMerges)
	fmt.Fprintf(&b, "dependencies: gating_violations=%d propagation_violations=%d blocked_dependents=%d\n",
		a.Metrics.GatingViolations, a.Metrics.PropagationViolations, a.Metrics.BlockedDependents)
	rows := append([]PlanningCaseMetrics(nil), a.Cases...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].CaseID < rows[j].CaseID })
	for _, c := range rows {
		status := "PASS"
		if !c.Pass {
			status = "FAIL"
		}
		if c.Status != "completed" {
			status = strings.ToUpper(c.Status)
		}
		fmt.Fprintf(&b, "  %-32s %-8s submitted=%d created=%d expected=%d delta=%+d missed=%d false_merge=%d gate=%d prop=%d\n",
			c.CaseID, status, c.Submitted, c.IntentsCreated, c.ExpectedLogicalIntents, c.LogicalIntentDelta,
			c.MissedCollapses, c.FalseMerges, c.GatingViolations, c.PropagationViolations)
	}
	return b.String()
}
