package benchmark

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/evidence"
	"github.com/Autumn-27/artex/server"
	"github.com/Autumn-27/norma/llm"
)

// runner.go — executes benchmark cases through the REAL ARTEX pipeline.
//
// Offline mode: scripted provider (mock agent) — the engine is real; the LLM
// is scripted (§5). Live mode: provider built from llm_profiles (already
// authorized, no new integration).
//
// Each case runs through server.Engine (real planner loop + worker pool) with
// fresh-engine agents wired via SetAgentResolver. The real server.Task
// registry, ExplorationStore and evidence store are the data plane.
//
// Database policy (§16): callers pass a disposable DSN; no user databases are
// touched.

type Runner struct {
	mgr      *server.Manager
	pg       *db.DB
	engine   *server.Engine
	evidence *evidence.Store
	dir      string
	provider llm.Provider
}

func NewRunner(dir string) (*Runner, error) {
	mgr, err := server.NewManager(dir, "")
	if err != nil {
		return nil, fmt.Errorf("open manager: %w", err)
	}
	pg := mgr.PG()
	r := &Runner{mgr: mgr, pg: pg, engine: server.NewEngine(mgr), evidence: evidence.New(pg, nil, filepath.Join(dir, "evidence")), dir: dir}
	r.engine.SetAgentResolver(func(_ *server.Task) (*agent.Planner, *agent.Worker) {
		p := agent.NewPlanner(r.provider, "mock", dir, nil, 0, 0)
		w := agent.NewWorker(r.provider, "mock", dir, nil, 0, 0)
		p.SetFindingRecorder(r.evidence)
		w.SetFindingRecorder(r.evidence)
		return p, w
	})
	r.engine.SetReadiness(func() bool { return true })
	return r, nil
}

func (r *Runner) SetProvider(p llm.Provider) { r.provider = p }

func (r *Runner) Close() error { return r.mgr.Close() }

// LiveProvider builds the live-mode provider from the active llm_profiles
// entry (already configured/authorized; no new integration).
func LiveProvider(pg *db.DB) (llm.Provider, string, string, error) {
	prof, err := pg.ActiveProfile()
	if err != nil || prof == nil {
		return nil, "", "", errors.New("no active LLM profile configured (live mode requires one)")
	}
	cfg := llm.Config{Format: llm.Format(prof.Format), BaseURL: prof.BaseURL, APIKey: prof.APIKey, Model: prof.Model}
	prov, err := llm.NewProvider(cfg)
	if err != nil {
		return nil, "", "", err
	}
	return prov, prof.Name, prof.Model, nil
}

func intentTerminal(state string) bool {
	return state == "done" || state == "blocked" || state == "exhausted" || state == "stopped"
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// RunCase executes one case and returns its metrics. Engine/execution failures
// are represented in CaseMetrics.Status/Errors, not returned as error; error is
// reserved for harness-level failures (no PG, task creation failure).
func (r *Runner) RunCase(ctx context.Context, c *Case) (CaseMetrics, error) {
	res := CaseMetrics{CaseID: c.ID}
	dedupBefore := db.FindingDedupStats()
	start := time.Now()
	if r.provider == nil {
		res.Status = "execution_error"
		res.Errors = append(res.Errors, "benchmark: provider required")
		return res, errors.New("benchmark: provider required")
	}
	if c.ExecutionLimits.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.ExecutionLimits.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	st, err := r.mgr.CreateTaskWithOptions(fmt.Sprintf("bench: %s", c.ID), fmt.Sprintf("执行基准用例 %s 并如实记录真实发现。", c.ID), db.TaskCreateOptions{TimeoutSeconds: c.ExecutionLimits.TimeoutSeconds, PlanHeartbeatSeconds: 600})
	if err != nil {
		res.Status = "execution_error"
		res.Errors = append(res.Errors, err.Error())
		return res, nil
	}
	res.TaskID = st.ID
	taskID := st.ID
	ts := r.pg.Exploration(st.ExpID)
	maxIntents := c.ExecutionLimits.MaxIntents
	if maxIntents <= 0 {
		maxIntents = 1
	}
	intentIDs := make([]int64, 0, maxIntents)
	for i := 0; i < maxIntents; i++ {
		id, err := ts.AddIntent(map[string]any{"summary": fmt.Sprintf("基准用例 %s 的意图 #%d", c.ID, i)}, i, nil, "planner")
		if err != nil {
			res.Status = "execution_error"
			res.Errors = append(res.Errors, fmt.Sprintf("seed intent: %v", err))
			return res, nil
		}
		intentIDs = append(intentIDs, id)
	}

	r.engine.Run(ctx, st)
	allTerminal := waitCaseTerminal(ctx, r, ts, intentIDs)

	if ctx.Err() != nil && !allTerminal {
		res.Status = "timed_out"
	} else if st2, ok := r.mgr.Task(taskID); ok && db.IsTerminal(st2.Status) {
		res.Status = "completed"
	} else if st2, ok := r.mgr.Task(taskID); ok && st2.Status == "failed" {
		res.Status = "execution_error"
	} else if allTerminal {
		res.Status = "completed"
	} else {
		res.Status = "incomplete"
	}

	tCollect := time.Now()
	r.collectFindings(ctx, ts, intentIDs, &res, c, start)
	res.Timings.FindingValidationTime = computeTimingStats([]time.Duration{time.Since(tCollect)})
	mr := MatchFindings(c.ExpectedFindings, res.ActualFindings)
	res.MatchResult = mr
	res.CM.FromMatchResult(mr)
	res.Metrics = ComputeMetrics(res.CM, mr, res.ActualFindings, len(c.ExpectedFindings))
	dedupAfter := db.FindingDedupStats()
	res.Dedup = DedupMetrics{
		Submissions:         dedupAfter.Submissions - dedupBefore.Submissions,
		New:                 dedupAfter.New - dedupBefore.New,
		Duplicates:          dedupAfter.Duplicates - dedupBefore.Duplicates,
		EvidenceAttachments: dedupAfter.EvidenceAttachments - dedupBefore.EvidenceAttachments,
		Conflicts:           dedupAfter.Conflicts - dedupBefore.Conflicts,
		Errors:              dedupAfter.Errors - dedupBefore.Errors,
	}
	res.Timings.TotalTaskDuration = computeTimingStats([]time.Duration{time.Since(start)})
	return res, nil
}

// waitCaseTerminal polls until all seeded intents reach terminal states or the
// case context ends.
func waitCaseTerminal(ctx context.Context, r *Runner, ts *db.ExplorationStore, intentIDs []int64) bool {
	for {
		all := true
		for _, id := range intentIDs {
			n, err := ts.GetNode(id)
			if err != nil || n == nil || !intentTerminal(n.State) {
				all = false
				break
			}
		}
		if all {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// collectFindings gathers observations from the graph + findings table +
// telemetry (offline: activity; live: llm_usage), and times the evidence sweep
// (§E1 "evidence processing duration" / "finding validation").
func (r *Runner) collectFindings(ctx context.Context, ts *db.ExplorationStore, intentIDs []int64, res *CaseMetrics, c *Case, start time.Time) {
	actsAll := map[int64][]db.Activity{}
	for _, intentID := range intentIDs {
		acts, _, err := ts.ActivityList(&intentID, 0, 5000)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("activity list intent %d: %v", intentID, err))
			continue
		}
		actsAll[intentID] = acts
	}

	// Aggregate activity-based model usage (offline mode). Events are ordered
	// oldest-first; gaps between consecutive activities approximate durations.
	var planDur, workDur, toolDur []time.Duration
	for _, acts := range actsAll {
		ordered := orderActivities(acts)
		var prev *db.Activity
		for i := range ordered {
			a := ordered[i]
			switch {
			case strings.HasPrefix(a.Worker, "planner") || strings.HasPrefix(a.Worker, "mainagent"):
				if prev != nil && strings.HasPrefix(prev.Worker, "planner") {
					planDur = append(planDur, a.CreatedAt.Sub(prev.CreatedAt))
				}
			case strings.HasPrefix(a.Worker, "work#"):
				if prev != nil && strings.HasPrefix(prev.Worker, "work#") {
					workDur = append(workDur, a.CreatedAt.Sub(prev.CreatedAt))
				}
			case a.Kind == "tool_use" && i+1 < len(ordered):
				toolDur = append(toolDur, ordered[i+1].CreatedAt.Sub(a.CreatedAt))
			}
			if a.Kind == "tool_use" {
				res.ToolInvocations++
			} else if a.Kind == "tool_result" && a.IsError {
				res.ToolFailures++
			}
			// Offline model-usage aggregation (§E2): usage activities carry the
			// cumulative token counts of each model turn (agent/capture.go).
			if a.Kind == "usage" && a.InputTokens != nil {
				res.ModelUsage.Calls++
				res.ModelUsage.InputTokens += *a.InputTokens
				res.ModelUsage.OutputTokens += derefInt(a.OutputTokens)
				res.ModelUsage.CacheReadTokens += derefInt(a.CacheReadTokens)
				res.ModelUsage.CacheWriteTokens += derefInt(a.CacheWriteTokens)
				if res.ModelUsage.CallsPerLane == nil {
					res.ModelUsage.CallsPerLane = map[string]int{}
				}
				res.ModelUsage.CallsPerLane[a.Worker]++
			}
			if a.Kind == "result" && a.InputTokens != nil && a.IsError && (strings.HasPrefix(a.Worker, "planner") || strings.HasPrefix(a.Worker, "work#")) {
				res.ModelUsage.ProviderErrors++
			}
			prev = &a
		}
	}
	res.ModelUsage.TotalTokens = res.ModelUsage.InputTokens + res.ModelUsage.OutputTokens
	res.ModelUsage.UsageMetadataAvailable = res.ModelUsage.Calls > 0
	res.Timings.PlannerModelTime = computeTimingStats(planDur)
	res.Timings.WorkerModelTime = computeTimingStats(workDur)
	res.Timings.ToolExecutionTime = computeTimingStats(toolDur)

	nodes, _, err := ts.ListByKindPage(db.KindFinding, 0, 500)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("list findings: %v", err))
	}
	evStart := time.Now()
	for _, node := range nodes {
		findingID, err := r.pg.FindingIDByNodeID(node.ID)
		if err != nil || findingID == 0 {
			res.Errors = append(res.Errors, fmt.Sprintf("finding id lookup node=%d: %v", node.ID, err))
			continue
		}
		f, err := r.pg.GetFinding(findingID)
		if err != nil || f == nil {
			res.Errors = append(res.Errors, fmt.Sprintf("get finding %d: %v", findingID, err))
			continue
		}
		v, verr := r.evidence.ValidateFindingEvidence(ctx, findingID)
		state := "unknown"
		if verr == nil {
			state = string(v.State)
		}
		if state == "invalid" || state == "unknown" {
			res.EvidenceValidationFailures++
		}
		res.ActualFindings = append(res.ActualFindings, NodeToActualFinding(node, findingID, state, f != nil && strings.TrimSpace(f.Evidence) != ""))
		res.Timings.EvidenceValidationTime.Samples = append(res.Timings.EvidenceValidationTime.Samples, time.Since(evStart))
		evStart = time.Now()
	}
	sortActual(res.ActualFindings)
	_ = c
	_ = start
}

// orderActivities returns activities sorted by created_at (ascending; defensive
// for any list ordering).
func orderActivities(acts []db.Activity) []db.Activity {
	out := append([]db.Activity(nil), acts...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func sortActual(afs []ActualFinding) {
	for i := 1; i < len(afs); i++ {
		for j := i; j > 0 && afs[j].FindingID < afs[j-1].FindingID; j-- {
			afs[j], afs[j-1] = afs[j-1], afs[j]
		}
	}
}

// LiveModelUsage collects the token ledger from llm_usage (live mode, §E2-E3).
func LiveModelUsage(pg *db.DB, taskID string) ModelUsage {
	out := ModelUsage{UsageMetadataAvailable: false}
	rows, err := pg.Query("SELECT worker, count(*) n, coalesce(sum(input_tokens),0), coalesce(sum(output_tokens),0), coalesce(sum(cache_read),0), coalesce(sum(cache_write),0), coalesce(sum(status='error'),0), coalesce(max(profile_name),''), coalesce(max(model),'') FROM llm_usage WHERE task_id=$1 GROUP BY worker", taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var w string
		var n, in, outT, cr, cw, errs int
		var prof, model string
		if err := rows.Scan(&w, &n, &in, &outT, &cr, &cw, &errs, &prof, &model); err != nil {
			continue
		}
		out.Calls += n
		out.InputTokens += in
		out.OutputTokens += outT
		out.CacheReadTokens += cr
		out.CacheWriteTokens += cw
		out.ProviderErrors += errs
		if out.Profile == "" {
			out.Profile = prof
		}
		if out.Model == "" {
			out.Model = model
		}
		if out.CallsPerLane == nil {
			out.CallsPerLane = map[string]int{}
		}
		out.CallsPerLane[w] += n
	}
	out.TotalTokens = out.InputTokens + out.OutputTokens
	out.UsageMetadataAvailable = out.Calls > 0
	return out
}
