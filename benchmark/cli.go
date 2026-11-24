package benchmark

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/server"
	"github.com/Autumn-27/norma/llm"
)

// cli.go — `artex benchmark <cmd>` integration (§H). Commands:
//   list     — list suites/cases (§H1)
//   validate — validate suite/case ground truth (§H1)
//   run      — run a suite, write artifacts, print summary (§H2-H4)
//   compare  — compare two artifacts (§G2, §H)
//   report   — human-readable summary of an artifact

const usageText = `Usage:
  artex benchmark list [-suite ID]
  artex benchmark validate [-all] [-suite ID] [-case ID]
  artex benchmark run -suite ID [-mode offline|live] [-dsn DSN] [-out DIR] [-seed N]
  artex benchmark compare -runs A,B [-out FILE] [-verbose]
  artex benchmark report -run FILE [-out FILE] [-summary]

All commands are read-only except run/report writing to artifact directories.`

// Run is the benchmark subcommand dispatcher. Returns the process exit code.
func Run(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Println(usageText)
		return 0
	}
	switch args[0] {
	case "list":
		return cmdList(args[1:])
	case "validate":
		return cmdValidate(args[1:])
	case "run":
		return cmdRun(args[1:])
	case "compare":
		return cmdCompare(args[1:])
	case "report":
		return cmdReport(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown benchmark command: %s\n%s\n", args[0], usageText)
		return 2
	}
}

// parseFlags parses command flags with defaults.
func parseFlags(cmd string, args []string, defs map[string]string) (*flag.FlagSet, map[string]*string, error) {
	fs := flag.NewFlagSet("benchmark "+cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	vals := map[string]*string{}
	for name, def := range defs {
		vals[name] = fs.String(name, def, "")
	}
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	return fs, vals, nil
}

func fv(vals map[string]*string, k string) string {
	if p, ok := vals[k]; ok && p != nil {
		return *p
	}
	return ""
}

func cmdList(args []string) int {
	_, vals, _ := parseFlags("list", args, map[string]string{"suite": ""})
	for _, s := range DefaultSuites {
		if fv(vals, "suite") != "" && fv(vals, "suite") != s.SuiteID {
			continue
		}
		fmt.Printf("suite=%s version=%s cases=%d\n", s.SuiteID, s.SuiteVersion, len(s.Cases))
		for _, c := range s.Cases {
			fmt.Printf("  case=%s category=%s tags=%v\n", c.ID, c.Category, c.Tags)
		}
	}
	for _, s := range planningSuites {
		if fv(vals, "suite") != "" && fv(vals, "suite") != s.SuiteID {
			continue
		}
		fmt.Printf("suite=%s version=%s cases=%d runner=planning\n", s.SuiteID, s.SuiteVersion, len(s.Cases))
		for _, c := range s.Cases {
			fmt.Printf("  case=%s category=%s\n", c.ID, c.Category)
		}
	}
	return 0
}

func cmdValidate(args []string) int {
	verbose := false
	filtered := []string{}
	for _, a := range args {
		if a == "-all" || a == "-verbose" {
			verbose = true
			continue
		}
		filtered = append(filtered, a)
	}
	_, vals, _ := parseFlags("validate", filtered, map[string]string{"suite": "", "case": ""})
	var errs []string
	for _, s := range DefaultSuites {
		if fv(vals, "suite") != "" && s.SuiteID != fv(vals, "suite") {
			continue
		}
		errs = append(errs, s.Validate()...)
		for _, c := range s.Cases {
			if fv(vals, "case") != "" && c.ID != fv(vals, "case") {
				continue
			}
			if fv(vals, "case") == "" {
				// already covered by suite.Validate
			}
		}
	}
	for _, s := range planningSuites {
		if fv(vals, "suite") != "" && s.SuiteID != fv(vals, "suite") {
			continue
		}
		errs = append(errs, s.Validate()...)
	}
	if verbose && len(errs) == 0 {
		fmt.Printf("OK (validated %d cases in suite %s)\n", len(DefaultSuites[0].Cases), DefaultSuites[0].SuiteID)
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "validation: %s\n", e)
		}
		return 2
	}
	fmt.Printf("OK (%d suites validated)\n", len(DefaultSuites))
	return 0
}

func cmdRun(args []string) int {
	_, vals, err := parseFlags("run", args, map[string]string{
		"suite": "artex-v5-offline-baseline", "mode": "offline", "dsn": "", "out": "", "seed": "",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "usage error: %v\n%s\n", err, usageText)
		return 2
	}
	mode := fv(vals, "mode")
	if ps := PlanningSuiteByID(fv(vals, "suite")); ps != nil {
		return cmdRunPlanning(ps, mode, fv(vals, "dsn"), fv(vals, "out"), fv(vals, "seed"))
	}
	suite := SuiteByID(fv(vals, "suite"))
	if suite == nil {
		fmt.Fprintf(os.Stderr, "unknown suite: %s\n%s\n", fv(vals, "suite"), usageText)
		return 2
	}
	if errs := suite.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "validation: %s\n", e)
		}
		return 2
	}
	dsn := fv(vals, "dsn")
	if dsn == "" {
		dsn = os.Getenv("ARTEX_PG_DSN")
	}
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "no DSN: pass -dsn or set ARTEX_PG_DSN (must be a disposable benchmark DB)")
		return 2
	}

	dir := fv(vals, "out")
	if dir == "" {
		dir = filepath.Join("benchmark-artifacts", NewRunID(mode))
	}
	_ = os.MkdirAll(dir, 0o755)
	seed := time.Now().UnixNano()
	if s := fv(vals, "seed"); s != "" {
		if v, perr := strconv.ParseInt(s, 10, 64); perr == nil {
			seed = v
		}
	}
	runStart := time.Now().UTC()
	_ = os.Setenv("ARTEX_PG_DSN", dsn)
	_, configFP, redactions := EnvironmentSnapshot()
	gitHead := gitOutput("rev-parse", "HEAD")
	worktreeFingerprint := gitOutput("status", "--short")
	runner, err := NewRunner(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runner: %v\n", err)
		return 2
	}
	defer runner.Close()

	ctx := context.Background()
	results := make([]CaseMetrics, 0, len(suite.Cases))
	allOK := true
	for _, c := range suite.Cases {
		r := CaseMetrics{CaseID: c.ID, Mode: mode}
		var prov llm.Provider
		var prof, model string
		if mode == "offline" {
			prov = NewScriptedProvider(c.Script)
		} else {
			p, pn, md, lerr := LiveProvider(runner.pg)
			if lerr != nil {
				r.Status = "execution_error"
				r.Errors = append(r.Errors, "live provider: "+lerr.Error())
				allOK = false
				results = append(results, r)
				continue
			}
			prov, prof, model = p, pn, md
		}
		runner.SetProvider(prov)
		res, rerr := runner.RunCase(ctx, c)
		if mode == "live" {
			res.ModelUsage = LiveModelUsage(runner.pg, res.TaskID)
			res.ModelUsage.Profile = prof
			res.ModelUsage.Model = model
		}
		if rerr != nil {
			res.Status = "execution_error"
			res.Errors = append(res.Errors, rerr.Error())
			allOK = false
		}
		if res.Status != "completed" {
			allOK = false
		}
		results = append(results, res)
	}

	sm := ComputeSuiteMetrics(suite, results, mode)
	manifest := RunManifest{
		RunID:           NewRunID(mode),
		Mode:            mode,
		Seed:            seed,
		BenchmarkSuite:  suite.SuiteID,
		SuiteVersion:    suite.SuiteVersion,
		Cases:           len(results),
		Version:         server.BuildVersion,
		Os:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		GoVersion:       runtime.Version(),
		ExecutionMode:   "benchmark-runner",
		StartedAt:       runStart,
		FinishedAt:      time.Now().UTC(),
		DurationSecs:    time.Since(runStart).Seconds(),
		ConfigFingerprint: configFP,
		Redactions:       redactions,
		GitHead:          gitHead,
		TreeFingerprint:  worktreeFingerprint,
		WorktreeEntries:  len(strings.Split(worktreeFingerprint, "\n")),
	}
	if errs := manifest.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "manifest: %s\n", e)
		}
	}
	art := RunArtifact{Manifest: manifest, Metrics: sm, Cases: results}
	if errs := art.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "artifact validation: %s\n", e)
		}
		return 2
	}
	artifactPath := filepath.Join(dir, "run.json")
	if err := art.Save(artifactPath); err != nil {
		fmt.Fprintf(os.Stderr, "save artifact: %v\n", err)
		return 2
	}
	printSummary(&sm, results)
	fmt.Printf("\nartifact: %s\n", artifactPath)
	if !allOK {
		fmt.Println("status: INCOMPLETE — some cases did not complete (see artifact)")
		return 1
	}
	fmt.Println("status: OK — all cases completed")
	return 0
}

func cmdCompare(args []string) int {
	verbose := false
	filtered := []string{}
	for _, a := range args {
		if a == "-verbose" {
			verbose = true
			continue
		}
		filtered = append(filtered, a)
	}
	_, vals, err := parseFlags("compare", filtered, map[string]string{"runs": "", "out": ""})
	if err != nil {
		fmt.Fprintf(os.Stderr, "usage error: %v\n%s\n", err, usageText)
		return 2
	}
	runs := strings.Split(fv(vals, "runs"), ",")
	if len(runs) != 2 || strings.TrimSpace(runs[0]) == "" || strings.TrimSpace(runs[1]) == "" {
		fmt.Fprintln(os.Stderr, "compare requires -runs A,B")
		return 2
	}
	a, err := LoadRunArtifact(strings.TrimSpace(runs[0]))
	if err != nil {
		fmt.Fprintf(os.Stderr, "load A: %v\n", err)
		return 2
	}
	b, err := LoadRunArtifact(strings.TrimSpace(runs[1]))
	if err != nil {
		fmt.Fprintf(os.Stderr, "load B: %v\n", err)
		return 2
	}
	cmp := ComputeComparison(a, b)
	if out := fv(vals, "out"); out != "" {
		b2, _ := json.MarshalIndent(cmp, "", "  ")
		_ = os.WriteFile(out, append(b2, '\n'), 0o600)
		fmt.Printf("comparison: %s\n", out)
	}
	fmt.Printf("runs: %s vs %s (suite %s@%s vs %s@%s, mode %s vs %s)\n",
		cmp.RunA, cmp.RunB, cmp.SuiteA, cmp.VersionA, cmp.SuiteB, cmp.VersionB, cmp.ModeA, cmp.ModeB)
	fmt.Printf("incompatible: %s\n", cmp.Incompatible)
	var totLostTP, totGainedTP, totLostFP, totGainedFP int
	for _, cc := range cmp.Cases {
		totLostTP += cc.LostTP
		totGainedTP += cc.GainedTP
		totLostFP += cc.LostFPsA
		totGainedFP += cc.GainedFPsB
		fmt.Printf("  case=%s status=%s→%s TP=%d→%d FP=%d→%d FN(A)=%d FN(B)=%d\n",
			cc.CaseID, cc.StatusA, cc.StatusB, cc.A.CM.TP, cc.B.CM.TP, cc.A.CM.FP, cc.B.CM.FP, cc.A.CM.FN, cc.B.CM.FN)
		if verbose {
			for _, e := range cc.LostExpected {
				fmt.Printf("    - expected lost: %s\n", e.Fingerprint())
			}
			for _, e := range cc.GainedExpected {
				fmt.Printf("    + expected gained: %s\n", e.Fingerprint())
			}
			for _, af := range cc.LostFindings {
				fmt.Printf("    - finding lost: %s (%s)\n", af.Fingerprint, af.EvidenceState)
			}
			for _, af := range cc.GainedFindings {
				fmt.Printf("    + finding gained: %s (%s)\n", af.Fingerprint, af.EvidenceState)
			}
		}
	}
	fmt.Printf("totals: TP %d→%d (Δ+%d/−%d) FP %d→%d (Δ+%d/−%d)\n",
		cmp.MetricsA.TP, cmp.MetricsB.TP, totGainedTP, totLostTP, cmp.MetricsA.FP, cmp.MetricsB.FP, totGainedFP, totLostFP)
	if cmp.Incompatible != "ok" {
		return 2
	}
	return 0
}

func cmdReport(args []string) int {
	summary := false
	filtered := []string{}
	for _, a := range args {
		if a == "-summary" {
			summary = true
			continue
		}
		filtered = append(filtered, a)
	}
	_, vals, _ := parseFlags("report", filtered, map[string]string{"run": "", "out": ""})
	run := fv(vals, "run")
	if run == "" {
		fmt.Fprintln(os.Stderr, "report requires -run FILE")
		return 2
	}
	ra, err := LoadRunArtifact(run)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 2
	}
	if errs := ra.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "artifact validation: %s\n", e)
		}
	}
	printSummary(&ra.Metrics, ra.Cases)
	if summary {
		b, _ := json.MarshalIndent(ra.Manifest, "", "  ")
		fmt.Println(string(b))
	}
	if out := fv(vals, "out"); out != "" {
		var b strings.Builder
		fmt.Fprintf(&b, "# ARTEX v5 benchmark report — %s\n\n", ra.Manifest.RunID)
		fmt.Fprintf(&b, "- mode: %s, suite: %s@%s, version: %s\n", ra.Manifest.Mode, ra.Manifest.BenchmarkSuite, ra.Manifest.SuiteVersion, ra.Manifest.Version)
		fmt.Fprintf(&b, "- git head: %s\n\n", ra.Manifest.GitHead)
		fmt.Fprintf(&b, "| case | status | TP | FP | TN | FN | precision | recall | evidence |\n|---|---|---|---|---|---|---|---|---|\n")
		for _, c := range ra.Cases {
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %.3f | %.3f | %.3f |\n",
				c.CaseID, c.Status, c.Metrics.TP, c.Metrics.FP, c.Metrics.TN, c.Metrics.FN, c.Metrics.Precision, c.Metrics.Recall, c.Metrics.EvidenceValidityRate)
		}
		_ = os.WriteFile(out, []byte(b.String()), 0o600)
		fmt.Printf("report written: %s\n", out)
	}
	return 0
}

func printSummary(sm *SuiteMetrics, cases []CaseMetrics) {
	fmt.Printf("suite=%s mode=%s cases=%d\n", sm.SuiteID, sm.Mode, len(cases))
	fmt.Printf("micro: TP=%d FP=%d TN=%d FN=%d precision=%.3f recall=%.3f f1=%.3f specificity=%.3f\n",
		sm.Micro.TP, sm.Micro.FP, sm.Micro.TN, sm.Micro.FN, sm.Micro.Precision, sm.Micro.Recall, sm.Micro.F1, sm.Micro.Specificity)
	fmt.Printf("case completion: %d/%d (%.1f%%) evidence validity: %.3f finding evidence coverage: %.3f\n",
		sm.CaseCompletionCount, len(cases), sm.Micro.CaseCompletionRate*100, sm.Micro.EvidenceValidityRate, sm.Micro.FindingEvidenceCoverage)
	fmt.Printf("reliability: tasks=%d completed, failed=%d timed_out=%d incomplete=%d, tools invoked=%d failed=%d, evidence-check failures=%d\n",
		sm.Reliability.TasksCompleted, sm.Reliability.TasksFailed, sm.Reliability.TasksTimedOut,
		sm.Reliability.IncompleteResults, sm.Reliability.ToolInvocations, sm.Reliability.ToolFailures, sm.Reliability.EvidenceValidationFailures)
	var dedup DedupMetrics
	for _, c := range cases {
		dedup.Submissions += c.Dedup.Submissions
		dedup.New += c.Dedup.New
		dedup.Duplicates += c.Dedup.Duplicates
		dedup.EvidenceAttachments += c.Dedup.EvidenceAttachments
		dedup.Conflicts += c.Dedup.Conflicts
		dedup.Errors += c.Dedup.Errors
	}
	fmt.Printf("finding dedup: submissions=%d new=%d duplicates=%d evidence_attached=%d db_conflicts=%d errors=%d\n",
		dedup.Submissions, dedup.New, dedup.Duplicates, dedup.EvidenceAttachments, dedup.Conflicts, dedup.Errors)
	if sm.ModelUsage.Calls > 0 {
		fmt.Printf("model usage: calls=%d input=%d output=%d total=%d provider_errors=%d (usage_metadata_available=%v)\n",
			sm.ModelUsage.Calls, sm.ModelUsage.InputTokens, sm.ModelUsage.OutputTokens, sm.ModelUsage.TotalTokens, sm.ModelUsage.ProviderErrors, sm.ModelUsage.UsageMetadataAvailable)
	}
	if t := sm.Timings.TotalTaskDuration; t.Count > 0 {
		fmt.Printf("timings: total=%s planner=%s worker=%s tool=%s evval=%s findval=%s\n",
			fmtDur(t.Mean), fmtDur(sm.Timings.PlannerModelTime.Mean), fmtDur(sm.Timings.WorkerModelTime.Mean),
			fmtDur(sm.Timings.ToolExecutionTime.Mean), fmtDur(sm.Timings.EvidenceValidationTime.Mean), fmtDur(sm.Timings.FindingValidationTime.Mean))
	}
	for _, c := range cases {
		fmt.Printf("  case=%-40s status=%-11s TP=%d FP=%d TN=%d FN=%d evidence=%.2f tools=%d/%d dur=%s\n",
			c.CaseID, c.Status, c.Metrics.TP, c.Metrics.FP, c.Metrics.TN, c.Metrics.FN, c.Metrics.EvidenceValidityRate, c.ToolInvocations, c.ToolFailures, fmtDur(c.Timings.TotalTaskDuration.Mean))
	}
}

func fmtDur(ms float64) string { return fmt.Sprintf("%.0fms", ms) }

// cmdRunPlanning runs a planning suite through the planning runner. Modes:
//   offline — real Planner.Plan path (scripted provider, real add_intent tool)
//   legacy  — pre-IX.2.2 write path (AddNode), for a reproducible before/after
func cmdRunPlanning(s *PlanningSuite, mode, dsn, outDir, seedRaw string) int {
	if mode == "" {
		mode = "offline"
	}
	if mode != "offline" && mode != "legacy" {
		fmt.Fprintf(os.Stderr, "planning suites support -mode offline|legacy (got %q)\n", mode)
		return 2
	}
	if errs := s.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "validation: %s\n", e)
		}
		return 2
	}
	if dsn == "" {
		dsn = os.Getenv("ARTEX_PG_DSN")
	}
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "no DSN: pass -dsn or set ARTEX_PG_DSN (must be a disposable benchmark DB)")
		return 2
	}
	dir := outDir
	if dir == "" {
		dir = filepath.Join("benchmark-artifacts", NewRunID(mode))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "artifact dir: %v\n", err)
		return 2
	}
	seed := time.Now().UnixNano()
	if seedRaw != "" {
		if v, err := strconv.ParseInt(seedRaw, 10, 64); err == nil {
			seed = v
		}
	}
	_ = os.Setenv("ARTEX_PG_DSN", dsn)
	runner, err := NewPlanningRunner(dir, mode == "legacy")
	if err != nil {
		fmt.Fprintf(os.Stderr, "planning runner: %v\n", err)
		return 2
	}
	defer runner.Close()

	art := runner.RunSuite(context.Background(), s, seed, time.Now().UTC())
	path := filepath.Join(dir, "planning_run.json")
	if err := art.Save(path); err != nil {
		fmt.Fprintf(os.Stderr, "save artifact: %v\n", err)
		return 2
	}
	fmt.Print(art.Summary())
	fmt.Printf("\nartifact: %s\n", path)
	if art.Metrics.Passed != art.Metrics.MeasuredCases {
		fmt.Println("status: FAIL — planning ground truth not satisfied (see artifact)")
		return 1
	}
	fmt.Println("status: OK — all measured planning cases satisfied ground truth")
	return 0
}
