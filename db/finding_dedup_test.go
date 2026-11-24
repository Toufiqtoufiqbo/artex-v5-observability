package db

import (
	"context"
	"sync"
	"testing"
)

// These integration tests use a fresh disposable PostgreSQL database supplied
// through ARTEX_PG_DSN. They never touch an existing user database.

func newDedupFixture(t *testing.T, marker string) (*DB, *Task, *ExplorationStore, string) {
	t.Helper()
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	t.Cleanup(func() { d.Close() })
	task, err := d.CreateTask("[ix212] "+marker, "dedup test goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Exec(`DELETE FROM findings WHERE task_id=$1`, task.ID) //nolint:errcheck
		d.DeleteTask(task.ID)                                    //nolint:errcheck
	})
	return d, task, d.Exploration(task.ExplorationID), marker
}

func countByFingerprint(t *testing.T, d *DB, fp string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM findings WHERE finding_fingerprint=$1`, fp).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestFindingDedupSequential_v921(t *testing.T) {
	d, task, es, marker := newDedupFixture(t, "sequential")
	ctx := context.Background()
	in := RecordFindingInput{TaskID: task.ID, VulnClass: marker, Name: "Blind SQL Injection (boolean)", Severity: SeverityHigh, Summary: "s", Evidence: "poc", Worker: "w1"}
	fp := FindingFingerprint(task.ID, in.VulnClass, in.Name, in.AssetIDs)

	first, err := es.RecordFinding(ctx, in)
	if first == nil || err != nil || first.Outcome != "new" {
		t.Fatalf("first: recorded=%+v err=%v", first, err)
	}
	second, err := es.RecordFinding(ctx, in)
	if second == nil || err != nil || second.Outcome != "duplicate" {
		t.Fatalf("second: recorded=%+v err=%v", second, err)
	}
	if first.FindingID != second.FindingID || first.NodeID != second.NodeID {
		t.Fatalf("duplicate did not reuse finding: %d/%d vs %d/%d", first.FindingID, first.NodeID, second.FindingID, second.NodeID)
	}
	if got := countByFingerprint(t, d, fp); got != 1 {
		t.Fatalf("rows for identity: want 1, got %d", got)
	}
}

func TestFindingDedupConcurrent_v921(t *testing.T) {
	_, task, es, marker := newDedupFixture(t, "concurrent")
	ctx := context.Background()
	in := RecordFindingInput{TaskID: task.ID, VulnClass: marker, Name: "Reflected XSS (payload echo)", Severity: SeverityMedium, Summary: "s", Evidence: "poc", Worker: "w"}

	const n = 12
	type result struct {
		id, node int64
		outcome  string
		err      error
	}
	results := make([]result, n)
	start := make(chan struct{}) // barrier — no sleeps
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			r, err := es.RecordFinding(ctx, in)
			if r != nil {
				results[i] = result{r.FindingID, r.NodeID, r.Outcome, err}
			} else {
				results[i] = result{err: err}
			}
		}(i)
	}
	close(start)
	wg.Wait()

	newCount, ids := 0, map[int64]bool{}
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("concurrent insert: %v", r.err)
		}
		if r.outcome == "new" {
			newCount++
		} else if r.outcome != "duplicate" {
			t.Fatalf("unexpected outcome %q", r.outcome)
		}
		ids[r.id] = true
	}
	if newCount != 1 {
		t.Fatalf("want exactly 1 new finding, got %d", newCount)
	}
	if len(ids) != 1 {
		t.Fatalf("want 1 logical finding id, got %d: %v", len(ids), ids)
	}
	fp := FindingFingerprint(task.ID, in.VulnClass, in.Name, in.AssetIDs)
	if got := countByFingerprint(t, dOrFatal(t, es), fp); got != 1 {
		t.Fatalf("rows for identity: want 1, got %d", got)
	}
}

// dOrFatal is only used by the concurrent test to reach the DB handle through
// the store; it is a helper so the test reads clearly.
func dOrFatal(t *testing.T, es *ExplorationStore) *DB {
	t.Helper()
	return es.db
}

func TestFindingDedupDistinctFindingsRemainDistinct_v921(t *testing.T) {
	d, task, es, marker := newDedupFixture(t, "distinct")
	ctx := context.Background()
	a1, err := d.Assets().UpsertRootDomain(UpsertRootDomainReq{Domain: "ix212-a.brandtest.example"})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := d.Assets().UpsertRootDomain(UpsertRootDomainReq{Domain: "ix212-b.brandtest.example"})
	if err != nil {
		t.Fatal(err)
	}
	type submission struct {
		name   string
		assets []int64
	}
	subs := []submission{
		{"SQLi (param id)", []int64{a1}},
		{"SQLi (param sort)", []int64{a1}}, // different parameter
		{"SQLi (param id)", []int64{a2}},   // different asset
	}
	ids := map[int64]bool{}
	for _, s := range subs {
		in := RecordFindingInput{TaskID: task.ID, VulnClass: marker, Name: s.name, Severity: SeverityHigh, Summary: "s", Evidence: "poc", Worker: "w", AssetIDs: s.assets}
		r, err := es.RecordFinding(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if r.Outcome != "new" {
			t.Fatalf("distinct finding %q was merged", s.name)
		}
		ids[r.FindingID] = true
	}
	if len(ids) != len(subs) {
		t.Fatalf("want %d distinct findings, got %d", len(subs), len(ids))
	}

	// Same identity in a different task must also remain distinct.
	other, err := d.CreateTask("[ix212] distinct-other", "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Exec(`DELETE FROM findings WHERE task_id=$1`, other.ID) //nolint:errcheck
		d.DeleteTask(other.ID)                                    //nolint:errcheck
	})
	in := RecordFindingInput{TaskID: other.ID, VulnClass: marker, Name: "SQLi (param id)", Severity: SeverityHigh, Summary: "s", Evidence: "poc", Worker: "w", AssetIDs: []int64{a1}}
	r, err := d.Exploration(other.ExplorationID).RecordFinding(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if ids[r.FindingID] {
		t.Fatal("same identity across two tasks was merged")
	}
}

func TestFindingDedupRetryIdempotent_v921(t *testing.T) {
	d, task, es, marker := newDedupFixture(t, "retry")
	ctx := context.Background()
	in := RecordFindingInput{TaskID: task.ID, VulnClass: marker, Name: "SSTI (eval)", Severity: SeverityHigh, Summary: "s", Evidence: "poc", Worker: "w", AssetIDs: nil}
	var first int64
	for i := 0; i < 3; i++ { // retry after an uncertain persistence response
		r, err := es.RecordFinding(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = r.FindingID
		} else if r.FindingID != first {
			t.Fatalf("retry created a new finding: %d != %d", r.FindingID, first)
		}
	}
	var findingsEvents int
	if err := d.QueryRow(`SELECT count(*) FROM notification_events WHERE finding_id=$1`, first).Scan(&findingsEvents); err == nil {
		if findingsEvents != 1 {
			t.Fatalf("retries produced %d notification events for one finding", findingsEvents)
		}
	}
}

func TestFindingDedupDirectInsertConflict_v921(t *testing.T) {
	d, task, _, marker := newDedupFixture(t, "direct-conflict")
	const n = 8
	ids := make([]int64, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ids[i], errs[i] = d.AddFinding(task.ID, 0, marker, "Endpoint collision", SeverityLow, "s", "poc", "w", []int64{9})
		}(i)
	}
	close(start)
	wg.Wait()
	first := ids[0]
	for i := range ids {
		if errs[i] != nil {
			t.Fatalf("AddFinding[%d]: %v", i, errs[i])
		}
		if ids[i] != first {
			t.Fatalf("direct concurrent inserts created different findings: %v", ids)
		}
	}
	fp := FindingFingerprint(task.ID, marker, "Endpoint collision", []int64{9})
	if got := countByFingerprint(t, d, fp); got != 1 {
		t.Fatalf("rows for identity: want 1, got %d", got)
	}
}
