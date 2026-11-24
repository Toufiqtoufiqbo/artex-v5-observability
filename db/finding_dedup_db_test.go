package db

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestFindingDedupDatabaseEnforcesUniqueness_v921(t *testing.T) {
	d, task, _, marker := newDedupFixture(t, "db-unique")
	fp := FindingFingerprint(task.ID, marker, "Direct uniqueness probe", []int64{99})
	insert := func() error {
		_, err := d.Exec(`INSERT INTO findings(task_id,vulnclass,name,severity,finding_fingerprint)
VALUES($1,$2,$3,$4,$5)`, task.ID, marker, "Direct uniqueness probe", SeverityHigh, fp)
		return err
	}
	if err := insert(); err != nil {
		t.Fatalf("first raw insert: %v", err)
	}
	err := insert()
	if err == nil {
		t.Fatal("database accepted a second row with the same identity")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("want unique violation 23505, got %T: %v", err, err)
	}
}

func TestFindingDedupUnrelatedErrorsStayVisible_v921(t *testing.T) {
	_, task, es, marker := newDedupFixture(t, "unrelated-error")
	// An intent_id from outside this task is an authorization/consistency error,
	// not a duplicate. It must surfaced as an error, never as a reused finding.
	in := RecordFindingInput{TaskID: task.ID, ExplorationID: task.ExplorationID, IntentID: 999999999, VulnClass: marker, Name: "Should fail", Severity: SeverityHigh, Summary: "s", Evidence: "poc"}
	r, err := es.RecordFinding(context.Background(), in)
	if err == nil {
		t.Fatalf("foreign intent was accepted: %+v", r)
	}
	if r != nil {
		t.Fatalf("an unrelated error must not return a logical finding: %+v", r)
	}
}
