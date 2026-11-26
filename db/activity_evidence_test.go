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

// Disposable ARTEX_PG_DSN database only; traffic ids are predictable, so only
// activity recorded in this exploration may corroborate a cited id.
func TestActivityCitesTrafficIDIsTaskLocalAndExact(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()

	current, err := d.CreateTask("[traffic-ref] current", "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	other, err := d.CreateTask("[traffic-ref] other", "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, task := range []*Task{current, other} {
			_, _ = d.Exec(`DELETE FROM activity WHERE exploration_id=$1`, task.ExplorationID)
			_ = d.DeleteTask(task.ID)
		}
	})

	const id = "1760000000-0007"
	const otherID = "1760000000-9999"
	insert := func(expID int64, kind, detail, metadata string) {
		t.Helper()
		if _, err := d.Exec(`INSERT INTO activity(exploration_id,kind,tool,detail,metadata) VALUES($1,$2,'traffic_get',$3,$4::jsonb)`,
			expID, kind, detail, metadata); err != nil {
			t.Fatal(err)
		}
	}
	// The id was returned by a tool result in this task.
	insert(current.ExplorationID, "tool_result", `{"id":"`+id+`","status":200}`, `{}`)
	// Another task saw a different exchange; it must not prove this one.
	insert(other.ExplorationID, "tool_result", `{"id":"`+otherID+`","status":200}`, `{}`)
	// A longer id containing ours as a prefix is not the same id.
	insert(current.ExplorationID, "tool_use", `{"id":"`+id+`9"}`, `{}`)

	ctx := context.Background()
	es := d.Exploration(current.ExplorationID)
	ok, err := es.ActivityCitesTrafficID(ctx, id)
	if err != nil || !ok {
		t.Fatalf("task-local traffic id was not found: ok=%v err=%v", ok, err)
	}
	ok, err = es.ActivityCitesTrafficID(ctx, otherID)
	if err != nil || ok {
		t.Fatalf("another task's traffic id was accepted: ok=%v err=%v", ok, err)
	}
	ok, err = es.ActivityCitesTrafficID(ctx, id+"9")
	if err != nil || !ok {
		t.Fatalf("exact longer id was not found: ok=%v err=%v", ok, err)
	}
	ok, err = es.ActivityCitesTrafficID(ctx, "")
	if err != nil || ok {
		t.Fatalf("empty id returned ok=%v err=%v", ok, err)
	}
}

func TestActivityCitesTrafficIDInMetadata(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()
	task, err := d.CreateTask("[traffic-ref] metadata", "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM activity WHERE exploration_id=$1`, task.ExplorationID)
		_ = d.DeleteTask(task.ID)
	})
	if _, err := d.Exec(`INSERT INTO activity(exploration_id,kind,tool,detail,metadata) VALUES($1,'tool_use','traffic_get','open',$2::jsonb)`,
		task.ExplorationID, `{"args":{"traffic_id":"1760000000-0042"}}`); err != nil {
		t.Fatal(err)
	}
	ok, err := d.Exploration(task.ExplorationID).ActivityCitesTrafficID(context.Background(), "1760000000-0042")
	if err != nil || !ok {
		t.Fatalf("traffic id in tool_use metadata was not found: ok=%v err=%v", ok, err)
	}
}
