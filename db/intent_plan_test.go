package db

import (
	"errors"
	"strconv"
	"testing"
)

// Phase IX.2.2 — full intent-management matrix (Gate D5, C3, F3).
// All tests run against a disposable PostgreSQL database (ARTEX_PG_DSN).

func hasIntentEdge(t *testing.T, es *ExplorationStore, src, dst int64, rel string) bool {
	t.Helper()
	var n int
	if err := es.db.QueryRow(`SELECT count(*) FROM exploration_edges WHERE exploration_id=$1 AND src_id=$2 AND dst_id=$3 AND rel=$4`,
		es.ID(), src, dst, rel).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func intentState(t *testing.T, es *ExplorationStore, id int64) string {
	t.Helper()
	n, err := es.GetNode(id)
	if err != nil || n == nil {
		t.Fatalf("get intent %d: %v", id, err)
	}
	return n.State
}

func plan(t *testing.T, es *ExplorationStore, req IntentPlanRequest) IntentPlanResult {
	t.Helper()
	res, err := es.AddIntentPlanned(req)
	if err != nil {
		t.Fatalf("AddIntentPlanned(%q): %v", req.Summary, err)
	}
	return res
}

// D1 — exact duplicate.
func TestIntentPlanExactDuplicate_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "exact")
	a := plan(t, es, IntentPlanRequest{Summary: "对 /login 做 SQL 注入测试"})
	b := plan(t, es, IntentPlanRequest{Summary: "对 /login 做 SQL 注入测试"})
	if a.Outcome != IntentOutcomeNew || b.Outcome != IntentOutcomeDuplicate || b.ID != a.ID {
		t.Fatalf("outcomes = %q/%q ids=%d/%d, want new/duplicate same id", a.Outcome, b.Outcome, a.ID, b.ID)
	}
}

// D1 — equivalent intent, different wording, same structured operation/resource.
func TestIntentPlanEquivalentWording_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "equivalent")
	a := plan(t, es, IntentPlanRequest{Summary: "测试 /login 的布尔盲注", Operation: "sqli-test", Resource: "/login?id=1"})
	b := plan(t, es, IntentPlanRequest{Summary: "SQL injection check on the login endpoint", Operation: "sqli-test", Resource: "/login?id=1"})
	if b.Outcome != IntentOutcomeDuplicate || b.ID != a.ID {
		t.Fatalf("equivalent wording not merged: %q id=%d (first %d)", b.Outcome, b.ID, a.ID)
	}
}

// D1 — same operation on different resources stays distinct.
func TestIntentPlanSameOperationDifferentResource_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "diff-resource")
	a := plan(t, es, IntentPlanRequest{Summary: "SQLi on login", Operation: "sqli-test", Resource: "/login?id=1"})
	b := plan(t, es, IntentPlanRequest{Summary: "SQLi on search", Operation: "sqli-test", Resource: "/search?q=1"})
	if a.ID == b.ID || b.Outcome != IntentOutcomeNew {
		t.Fatalf("different resources merged: %d/%d outcome=%q", a.ID, b.ID, b.Outcome)
	}
	if got := countIntents(t, es); got != 2 {
		t.Fatalf("rows = %d, want 2", got)
	}
}

// D1 — same resource with different parameters stays distinct.
func TestIntentPlanDifferentParameters_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "diff-params")
	a := plan(t, es, IntentPlanRequest{Summary: "IDOR on user id", Operation: "idor-test", Resource: "/api/user?id=1"})
	b := plan(t, es, IntentPlanRequest{Summary: "IDOR on user id", Operation: "idor-test", Resource: "/api/user?id=2"})
	if a.ID == b.ID {
		t.Fatalf("different parameters merged: %d", a.ID)
	}
}

// D1/D2 — explicit retest is allowed and linked, never silently suppressed.
func TestIntentPlanExplicitRetest_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "retest")
	first := plan(t, es, IntentPlanRequest{Summary: "测试上传绕过", Operation: "upload-bypass", Resource: "/upload"})
	if err := es.SetIntentState(first.ID, "done"); err != nil {
		t.Fatal(err)
	}
	retest := plan(t, es, IntentPlanRequest{Summary: "重测上传绕过（上次修复后）", Operation: "upload-bypass", Resource: "/upload", RetestOf: first.ID})
	if retest.Outcome != IntentOutcomeRetest || retest.ID == first.ID {
		t.Fatalf("retest outcome=%q id=%d (first %d)", retest.Outcome, retest.ID, first.ID)
	}
	if !hasIntentEdge(t, es, retest.ID, first.ID, RelRetestOf) {
		t.Fatal("retest_of edge missing")
	}
}

// D1/D2 — retry after transient failure is allowed and linked as retry_of.
func TestIntentPlanRetryAfterTransientFailure_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "retry-blocked")
	first := plan(t, es, IntentPlanRequest{Summary: "枚举 /api 端点"})
	if err := es.SetIntentState(first.ID, "blocked"); err != nil {
		t.Fatal(err)
	}
	retry := plan(t, es, IntentPlanRequest{Summary: "枚举 /api 端点"})
	if retry.Outcome != IntentOutcomeRetry || retry.ID == first.ID {
		t.Fatalf("retry outcome=%q id=%d", retry.Outcome, retry.ID)
	}
	if !hasIntentEdge(t, es, retry.ID, first.ID, RelRetryOf) {
		t.Fatal("retry_of edge missing")
	}
}

// D1/D2 — retry after uncertain completion (stopped).
func TestIntentPlanRetryAfterStopped_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "retry-stopped")
	first := plan(t, es, IntentPlanRequest{Summary: "探测 /admin 登录"})
	if err := es.SetIntentState(first.ID, "stopped"); err != nil {
		t.Fatal(err)
	}
	retry := plan(t, es, IntentPlanRequest{Summary: "探测 /admin 登录"})
	if retry.Outcome != IntentOutcomeRetry || !hasIntentEdge(t, es, retry.ID, first.ID, RelRetryOf) {
		t.Fatalf("stopped retry outcome=%q edge=%v", retry.Outcome, hasIntentEdge(t, es, retry.ID, first.ID, RelRetryOf))
	}
}

// D2 — duplicate while running is reused, not executed twice.
func TestIntentPlanDuplicateWhileRunning_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "dup-running")
	first := plan(t, es, IntentPlanRequest{Summary: "对 /checkout 做竞态测试"})
	ok, err := es.ClaimIntent(first.ID, "work#1")
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	dup := plan(t, es, IntentPlanRequest{Summary: "对 /checkout 做竞态测试"})
	if dup.Outcome != IntentOutcomeDuplicate || dup.ID != first.ID {
		t.Fatalf("running duplicate: outcome=%q id=%d want %d", dup.Outcome, dup.ID, first.ID)
	}
	if got := countIntents(t, es); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
}

// D2 — duplicate after completion creates a repeat with retest lineage.
func TestIntentPlanDuplicateAfterCompletion_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "dup-done")
	first := plan(t, es, IntentPlanRequest{Summary: "检测默认口令"})
	if err := es.SetIntentState(first.ID, "done"); err != nil {
		t.Fatal(err)
	}
	repeat := plan(t, es, IntentPlanRequest{Summary: "检测默认口令"})
	if repeat.Outcome != IntentOutcomeRepeat || repeat.ID == first.ID {
		t.Fatalf("repeat outcome=%q id=%d", repeat.Outcome, repeat.ID)
	}
	if !hasIntentEdge(t, es, repeat.ID, first.ID, RelRetestOf) {
		t.Fatal("repeat must be linked with retest_of")
	}
}

// D2 — duplicate after cancellation is a retry, not a silent suppression.
func TestIntentPlanDuplicateAfterCancellation_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "dup-deleted")
	first := plan(t, es, IntentPlanRequest{Summary: "探测备份文件"})
	if err := es.SetIntentState(first.ID, StateIntentDeleted); err != nil {
		t.Fatal(err)
	}
	again := plan(t, es, IntentPlanRequest{Summary: "探测备份文件"})
	if again.Outcome != IntentOutcomeRetry || again.ID == first.ID {
		t.Fatalf("deleted duplicate: outcome=%q id=%d", again.Outcome, again.ID)
	}
}

// D2 — duplicate after permanent failure (exhausted).
func TestIntentPlanDuplicateAfterExhausted_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "dup-exhausted")
	first := plan(t, es, IntentPlanRequest{Summary: "暴力枚举目录"})
	if err := es.SetIntentState(first.ID, "exhausted"); err != nil {
		t.Fatal(err)
	}
	again := plan(t, es, IntentPlanRequest{Summary: "暴力枚举目录"})
	if again.Outcome != IntentOutcomeRetry || !hasIntentEdge(t, es, again.ID, first.ID, RelRetryOf) {
		t.Fatalf("exhausted duplicate: outcome=%q", again.Outcome)
	}
}

// C3 — a dependent intent is not claimable until its prerequisite is done.
func TestIntentDependencyGating_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "dep-gate")
	pre := plan(t, es, IntentPlanRequest{Summary: "先拿到登录会话"})
	dep := plan(t, es, IntentPlanRequest{Summary: "用会话访问 /admin", DependsOn: []int64{pre.ID}})

	if ok, err := es.ClaimIntent(dep.ID, "work#1"); err != nil || ok {
		t.Fatalf("dependent claimed before prerequisite done: ok=%v err=%v", ok, err)
	}
	if err := es.SetIntentState(pre.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if ok, err := es.ClaimIntent(dep.ID, "work#1"); err != nil || !ok {
		t.Fatalf("dependent not claimable after prerequisite done: ok=%v err=%v", ok, err)
	}
}

// C3 — a failed prerequisite blocks its dependents (cascade, no deadlock).
func TestIntentDependencyFailureBlocksDependents_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "dep-fail")
	pre := plan(t, es, IntentPlanRequest{Summary: "先拿到登录会话"})
	dep1 := plan(t, es, IntentPlanRequest{Summary: "用会话访问 /admin", DependsOn: []int64{pre.ID}})
	dep2 := plan(t, es, IntentPlanRequest{Summary: "用会话访问 /billing", DependsOn: []int64{dep1.ID}})

	if err := es.SetIntentState(pre.ID, "blocked"); err != nil {
		t.Fatal(err)
	}
	n, err := es.BlockDependents(pre.ID, "prerequisite failed")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("blocked dependents = %d, want 2 (cascade)", n)
	}
	if s := intentState(t, es, dep1.ID); s != "blocked" {
		t.Fatalf("dep1 state = %q, want blocked", s)
	}
	if s := intentState(t, es, dep2.ID); s != "blocked" {
		t.Fatalf("dep2 state = %q, want blocked", s)
	}
	if ok, _ := es.ClaimIntent(dep1.ID, "work#1"); ok {
		t.Fatal("blocked dependent must not be claimable")
	}
}

// C3/D2 — reopening a prerequisite reopens the dependents it blocked.
func TestIntentDependencyReopenCascade_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "dep-reopen")
	pre := plan(t, es, IntentPlanRequest{Summary: "先拿到登录会话"})
	dep := plan(t, es, IntentPlanRequest{Summary: "用会话访问 /admin", DependsOn: []int64{pre.ID}})
	if err := es.SetIntentState(pre.ID, "blocked"); err != nil {
		t.Fatal(err)
	}
	if _, err := es.BlockDependents(pre.ID, "prerequisite #"+strconv.FormatInt(pre.ID, 10)+" ended blocked"); err != nil {
		t.Fatal(err)
	}
	if _, err := es.ReopenIntent(pre.ID); err != nil {
		t.Fatal(err)
	}
	if s := intentState(t, es, dep.ID); s != "open" {
		t.Fatalf("dependent state after prerequisite reopen = %q, want open", s)
	}
}

// C3 — a conflicting (cyclic) dependency graph is rejected.
func TestIntentDependencyCycleRejected_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "dep-cycle")
	a := plan(t, es, IntentPlanRequest{Summary: "A"})
	b := plan(t, es, IntentPlanRequest{Summary: "B"})
	// Inject a cycle directly (simulating a corrupted/imported graph).
	if err := es.Link(a.ID, RelDependsOn, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := es.Link(b.ID, RelDependsOn, a.ID); err != nil {
		t.Fatal(err)
	}
	_, err := es.AddIntentPlanned(IntentPlanRequest{Summary: "C", DependsOn: []int64{a.ID}})
	if !errors.Is(err, ErrIntentDependencyCycle) {
		t.Fatalf("cycle not rejected: %v", err)
	}
}

// D5.15 — invalid dependency references are rejected with a structured error.
func TestIntentDependencyValidation_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "dep-invalid")

	if _, err := es.AddIntentPlanned(IntentPlanRequest{Summary: "x", DependsOn: []int64{0}}); !errors.Is(err, ErrIntentInvalid) {
		t.Fatalf("non-positive dep: %v", err)
	}
	if _, err := es.AddIntentPlanned(IntentPlanRequest{Summary: "x", DependsOn: []int64{999999}}); !errors.Is(err, ErrIntentInvalid) {
		t.Fatalf("missing dep: %v", err)
	}
	goal, err := es.AddGoal(map[string]any{"text": "g"}, "system")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := es.AddIntentPlanned(IntentPlanRequest{Summary: "x", DependsOn: []int64{goal}}); !errors.Is(err, ErrIntentInvalid) {
		t.Fatalf("non-intent dep: %v", err)
	}
	if _, err := es.AddIntentPlanned(IntentPlanRequest{Summary: "x", RetestOf: 999999}); !errors.Is(err, ErrIntentInvalid) {
		t.Fatalf("missing retest_of: %v", err)
	}
}

// D5.16 — plan revision after new evidence: a materially different operation on
// the same resource is preserved as new work.
func TestIntentPlanRevisionAfterEvidence_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "revision")
	a := plan(t, es, IntentPlanRequest{Summary: "检测 /upload 上传限制", Operation: "upload-test", Resource: "/upload"})
	if err := es.SetIntentState(a.ID, "done"); err != nil {
		t.Fatal(err)
	}
	b := plan(t, es, IntentPlanRequest{Summary: "利用 /upload 绕过扩展名校验", Operation: "upload-bypass", Resource: "/upload"})
	if b.Outcome != IntentOutcomeNew || b.ID == a.ID {
		t.Fatalf("materially different follow-up merged: outcome=%q id=%d", b.Outcome, b.ID)
	}
}

// F3 — the administrative intent ceiling rejects new work with a structured error.
func TestIntentBudgetExhausted_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "budget")
	for i := 0; i < MaxIntentsPerExploration; i++ {
		if _, err := es.AddIntent(map[string]any{"summary": "budget filler " + strconv.FormatInt(int64(i), 10)}, 5, nil, "planner"); err != nil {
			t.Fatalf("filler %d: %v", i, err)
		}
	}
	_, err := es.AddIntentPlanned(IntentPlanRequest{Summary: "one more"})
	if !errors.Is(err, ErrIntentBudgetExhausted) {
		t.Fatalf("budget not enforced: %v", err)
	}
}
