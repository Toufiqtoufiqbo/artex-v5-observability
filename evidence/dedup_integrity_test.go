package evidence

import (
	"context"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// Gate E: deduplication must not weaken evidence/provenance guarantees.
func TestFindingDedupPreservesEvidenceIntegrity_v921(t *testing.T) {
	s, in, _ := evidenceFixture(t)
	ctx := context.Background()

	seedExchange(t, s, "dedup-a", []byte("baseline body"), false)
	seedExchange(t, s, "dedup-b", []byte("proof body"), false)
	refs := []db.TrafficRef{{TrafficID: "dedup-a", Role: "baseline", Note: "original baseline"}, {TrafficID: "dedup-b", Role: "proof", Note: "original proof"}}

	first, err := s.Record(ctx, in, refs)
	if err != nil || first.Outcome != "new" || len(first.Traffic.Bindings) != 2 {
		t.Fatalf("first record: recorded=%+v err=%v", first, err)
	}

	// A second observation of the SAME logical finding carries NEW evidence.
	seedExchange(t, s, "dedup-c", []byte("verification body"), false)
	dup, err := s.Record(ctx, in, []db.TrafficRef{{TrafficID: "dedup-c", Role: "verification", Note: "second observation"}})
	if err != nil || dup.Outcome != "duplicate" {
		t.Fatalf("duplicate record: recorded=%+v err=%v", dup, err)
	}
	if dup.FindingID != first.FindingID {
		t.Fatalf("duplicate created a new finding: %d != %d", dup.FindingID, first.FindingID)
	}
	if len(dup.Traffic.Bindings) != 3 {
		t.Fatalf("additional evidence was discarded: %d bindings", len(dup.Traffic.Bindings))
	}
	for _, b := range dup.Traffic.Bindings {
		switch b.Snapshot.SourceTrafficID {
		case "dedup-a":
			if b.Note != "original baseline" {
				t.Fatalf("weaker/second observation overwrote original note: %q", b.Note)
			}
		case "dedup-b":
			if b.Note != "original proof" {
				t.Fatalf("original proof note changed: %q", b.Note)
			}
		}
	}

	// Repeating an identical observation must not add another binding.
	again, err := s.Record(ctx, in, refs)
	if err != nil || again.Outcome != "duplicate" {
		t.Fatalf("repeat: recorded=%+v err=%v", again, err)
	}
	if len(again.Traffic.Bindings) != 3 {
		t.Fatalf("idempotent retry changed bindings: %+v", again.Traffic.Bindings)
	}

	// An invalid reference must remain an error even though the finding exists.
	if bad, err := s.Record(ctx, in, []db.TrafficRef{{TrafficID: "no-such-traffic", Role: "proof"}}); err == nil {
		t.Fatalf("invalid evidence was treated as valid duplicate: %+v", bad)
	}

	v, err := s.ValidateFindingEvidence(ctx, first.FindingID)
	if err != nil || v.State != EvidenceValid || v.Bindings != 3 {
		t.Fatalf("validity after dedup: %+v err=%v", v, err)
	}
}
