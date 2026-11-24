package db

import (
	"sync"
	"testing"
)

// Phase IX.2.2 — intent deduplication regression tests (application level).
//
// These tests exercise the single intent write path (ExplorationStore.AddIntent)
// on a disposable PostgreSQL database supplied through ARTEX_PG_DSN. They never
// touch an existing user database.
//
// Before IX.2.2 the store had no duplicate-intent check at all: two equivalent
// proposals produced two rows, and N concurrent producers produced N rows. The
// tests below were run against that pre-change code first (see the phase report
// §6 for the captured output), then against the implementation.

func newIntentFixture(t *testing.T, marker string) (*DB, *Task, *ExplorationStore) {
	t.Helper()
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	t.Cleanup(func() { d.Close() })
	task, err := d.CreateTask("[ix222] "+marker, "intent dedup test goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Exec(`DELETE FROM findings WHERE task_id=$1`, task.ID) //nolint:errcheck
		d.DeleteTask(task.ID)                                    //nolint:errcheck
	})
	return d, task, d.Exploration(task.ExplorationID)
}

func countIntents(t *testing.T, es *ExplorationStore) int {
	t.Helper()
	nodes, err := es.ListByKind(KindIntent, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return len(nodes)
}

func countIntentEdges(t *testing.T, es *ExplorationStore, rel string) int {
	t.Helper()
	var n int
	err := es.db.QueryRow(`SELECT count(*) FROM exploration_edges WHERE exploration_id=$1 AND rel=$2`, es.ID(), rel).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestIntentDedupSequential_v922: the same logical proposal submitted twice must
// resolve to one pending intent, and the second call must report reuse.
func TestIntentDedupSequential_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "sequential")

	first, err := es.AddIntent(map[string]any{"summary": "对 /login 做 SQL 注入测试"}, 5, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	second, err := es.AddIntent(map[string]any{"summary": "对 /login 做 SQL 注入测试"}, 5, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("duplicate proposal created a new intent: first=%d second=%d", first, second)
	}
	if got := countIntents(t, es); got != 1 {
		t.Fatalf("intent rows = %d, want 1", got)
	}
}

// TestIntentDedupNormalizedWording_v922: case/whitespace-only differences are the
// same proposal after canonicalisation.
func TestIntentDedupNormalizedWording_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "normalized")

	a, err := es.AddIntent(map[string]any{"summary": "Test   /admin for IDOR"}, 5, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	b, err := es.AddIntent(map[string]any{"summary": "test /admin for idor"}, 5, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("normalised duplicate created a new intent: %d != %d", a, b)
	}
}

// TestIntentDedupDistinctSummary_v922: genuinely different work is preserved.
func TestIntentDedupDistinctSummary_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "distinct")

	if _, err := es.AddIntent(map[string]any{"summary": "对 /login 做 SQL 注入测试"}, 5, nil, "planner"); err != nil {
		t.Fatal(err)
	}
	if _, err := es.AddIntent(map[string]any{"summary": "对 /upload 做文件上传绕过测试"}, 5, nil, "planner"); err != nil {
		t.Fatal(err)
	}
	if got := countIntents(t, es); got != 2 {
		t.Fatalf("distinct intents were merged: rows = %d, want 2", got)
	}
}

// TestIntentDedupAcrossExplorations_v922: scope is the exploration. Identical
// wording in two independent assessments must stay two intents.
func TestIntentDedupAcrossExplorations_v922(t *testing.T) {
	d, task, es := newIntentFixture(t, "scope-a")
	other, err := d.CreateTask("[ix222] scope-b", "second goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.DeleteTask(other.ID) }) //nolint:errcheck
	esB := d.Exploration(other.ExplorationID)
	_ = task

	if _, err := es.AddIntent(map[string]any{"summary": "枚举所有端点"}, 5, nil, "planner"); err != nil {
		t.Fatal(err)
	}
	if _, err := esB.AddIntent(map[string]any{"summary": "枚举所有端点"}, 5, nil, "planner"); err != nil {
		t.Fatal(err)
	}
	if got := countIntents(t, es); got != 1 {
		t.Fatalf("exploration A rows = %d, want 1", got)
	}
	if got := countIntents(t, esB); got != 1 {
		t.Fatalf("exploration B rows = %d, want 1 (cross-scope merge)", got)
	}
}

// TestIntentDedupConcurrent_v922: simultaneous proposals of the same intent must
// not create duplicate pending work. Barrier only — no sleeps.
func TestIntentDedupConcurrent_v922(t *testing.T) {
	_, _, es := newIntentFixture(t, "concurrent")

	const n = 8
	start := make(chan struct{})
	ids := make([]int64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ids[i], errs[i] = es.AddIntent(map[string]any{"summary": "对 /api/users 做越权测试"}, 5, nil, "planner")
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("concurrent duplicate produced distinct ids: %v", ids)
		}
	}
	if got := countIntents(t, es); got != 1 {
		t.Fatalf("intent rows = %d, want 1", got)
	}
}
