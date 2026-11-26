package db

import (
	"context"
	"testing"
)

// Disposable ARTEX_PG_DSN database only; never touches a user database.
func TestHasToolResultAnchorRequiresSameSuccessfulResult(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()
	task, err := d.CreateTask("[goal-gate] activity anchor", "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Exec(`DELETE FROM activity WHERE exploration_id=$1`, task.ExplorationID) //nolint:errcheck
		d.DeleteTask(task.ID)                                                      //nolint:errcheck
	})
	ins := func(kind string, isError bool, detail string) {
		t.Helper()
		if _, err := d.Exec(`INSERT INTO activity(exploration_id,kind,tool,summary,detail,is_error)
VALUES($1,$2,'HTTPRequest',$3,$4,$5)`, task.ExplorationID, kind, "result", detail, isError); err != nil {
			t.Fatal(err)
		}
	}
	ins("tool_result", false, "HTTP/1.1 200 OK")
	ins("tool_result", false, "GET /rest/user/login -> HTTP/1.1 401 Unauthorized")
	ins("tool_result", true, "POST /admin HTTP/1.1 200 attacker fabricated failure")

	es := d.Exploration(task.ExplorationID)
	ctx := context.Background()
	ok, err := es.HasToolResultAnchor(ctx, []string{"HTTP/1.1 401", "/rest/user/login"}, 2)
	if err != nil || !ok {
		t.Fatalf("real matching result was not found: ok=%v err=%v", ok, err)
	}
	// The status exists in one result, the path in another: not one artifact.
	ok, err = es.HasToolResultAnchor(ctx, []string{"HTTP/1.1 200", "/rest/user/login"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("anchors from two different tool results were treated as one artifact")
	}
	// Error results are not proof.
	ok, err = es.HasToolResultAnchor(ctx, []string{"/admin", "HTTP/1.1 200"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("a failed tool result was accepted as evidence")
	}
}
