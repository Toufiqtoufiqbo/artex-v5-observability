package db

import (
	"fmt"
	"testing"
	"time"
)

// TestListFindingsPageDedupGlobal proves the read path collapses the same
// vulnerability reported by two different tasks into one row, while the normal
// page keeps both per-task records (task evidence must never be lost).
func TestListFindingsPageDedupGlobal(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()

	marker := fmt.Sprintf("dedup-global-%d", time.Now().UnixNano())
	var tasks []int64
	for i := 1; i <= 2; i++ {
		tk, err := d.CreateTask(fmt.Sprintf("%s-%d", marker, i), "local fixture", nil, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, tk.ID)
		defer func(id int64) {
			_, _ = d.DeleteFindingsByTask(id)
			_ = d.DeleteTask(id)
		}(tk.ID)
	}

	name := marker + " SQL injection in /search q parameter"
	for _, id := range tasks {
		if _, err := d.AddFinding(id, 0, "sqli", name, SeverityHigh, "summary", "poc", "test", nil); err != nil {
			t.Fatal(err)
		}
	}

	filter := FindingFilter{VulnClass: "sqli", Query: marker}
	all, totalAll, err := d.ListFindingsPage(filter, 1, 50)
	if err != nil || totalAll != 2 || len(all) != 2 {
		t.Fatalf("normal page: rows=%d total=%d err=%v", len(all), totalAll, err)
	}

	filter.DedupGlobal = true
	uniq, totalUnique, err := d.ListFindingsPage(filter, 1, 50)
	if err != nil {
		t.Fatalf("dedup page: %v", err)
	}
	if totalUnique != 1 || len(uniq) != 1 {
		t.Fatalf("dedup page: rows=%d total=%d (want 1/1)", len(uniq), totalUnique)
	}
	if uniq[0].DuplicateCount != 2 {
		t.Fatalf("duplicate_count: want 2, got %d", uniq[0].DuplicateCount)
	}
}
