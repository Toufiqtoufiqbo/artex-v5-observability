package evidence

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// TestEvidenceValidityLifecycle_v863 is the F-B3 core: a finding is supported
// only while its bound bodies still hash to the recorded values. Tampering and
// deletion both invalidate it, every export format refuses it, the historical
// finding row survives, and restoring the exact bytes revalidates it.
func TestEvidenceValidityLifecycle_v863(t *testing.T) {
	s, in, _ := evidenceFixture(t)
	ctx := context.Background()
	body := []byte("synthetic evidence body for v8.6.3")
	seedExchange(t, s, "v863-life", body, false)
	r, err := s.Record(ctx, in, []db.TrafficRef{{TrafficID: "v863-life", Role: "proof"}})
	if err != nil {
		t.Fatal(err)
	}
	b := r.Traffic.Bindings[0]

	v, err := s.ValidateFindingEvidence(ctx, r.FindingID)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != EvidenceValid || v.Bindings != 1 || len(v.InvalidBindingIDs) != 0 {
		t.Fatalf("initial validity=%+v", v)
	}

	path, err := hashPath(s.Dir, b.Snapshot.RespHash)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), body...)
	tampered[0] ^= 0xff
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	v, err = s.ValidateFindingEvidence(ctx, r.FindingID)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != EvidenceInvalid || !slices.Contains(v.InvalidBindingIDs, b.ID) {
		t.Fatalf("tampered validity=%+v", v)
	}

	// CSV/JSON/md-single pass copyBodies=false; md-zip passes true. Both must
	// fail closed on invalid evidence.
	for _, copyBodies := range []bool{false, true} {
		err := s.StageFindingsExport(ctx, []*db.DBFinding{{ID: r.FindingID}}, t.TempDir(), copyBodies)
		if !errors.Is(err, ErrEvidenceInvalid) {
			t.Fatalf("export copyBodies=%v err=%v", copyBodies, err)
		}
	}

	// Historical observation is preserved, not rewritten or deleted.
	f, err := s.DB.GetFinding(r.FindingID)
	if err != nil || f == nil {
		t.Fatalf("historical finding lost: %v", err)
	}

	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	v, err = s.ValidateFindingEvidence(ctx, r.FindingID)
	if err != nil || v.State != EvidenceValid {
		t.Fatalf("restored validity=%+v err=%v", v, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	v, err = s.ValidateFindingEvidence(ctx, r.FindingID)
	if err != nil || v.State != EvidenceInvalid {
		t.Fatalf("missing validity=%+v err=%v", v, err)
	}
	if !strings.Contains(v.Detail, "缺失") {
		t.Fatalf("missing reason=%q", v.Detail)
	}
}

// TestEvidenceValidityMetadataTamper_v863 proves a database-only metadata edit
// is detected: the snapshot id is the digest of the normalized metadata.
func TestEvidenceValidityMetadataTamper_v863(t *testing.T) {
	s, in, _ := evidenceFixture(t)
	ctx := context.Background()
	seedExchange(t, s, "v863-meta", []byte("metadata proof"), false)
	r, err := s.Record(ctx, in, []db.TrafficRef{{TrafficID: "v863-meta"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`UPDATE traffic_evidence_snapshots SET url='https://evil.local/swapped' WHERE id=$1`, r.Traffic.Bindings[0].SnapshotID); err != nil {
		t.Fatal(err)
	}
	v, err := s.ValidateFindingEvidence(ctx, r.FindingID)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != EvidenceInvalid || !strings.Contains(v.Detail, "元数据") {
		t.Fatalf("metadata tamper not detected: %+v", v)
	}
	if err = s.StageFindingsExport(ctx, []*db.DBFinding{{ID: r.FindingID}}, t.TempDir(), false); !errors.Is(err, ErrEvidenceInvalid) {
		t.Fatalf("export accepted metadata tamper: %v", err)
	}
}

// TestEvidenceValidityRemovedBindings_v863: an authorized removal of the last
// binding must not leave the finding looking evidence-backed. The version is
// bumped, so zero bindings with a non-zero version is invalid.
func TestEvidenceValidityRemovedBindings_v863(t *testing.T) {
	s, in, _ := evidenceFixture(t)
	ctx := context.Background()
	seedExchange(t, s, "v863-rm", []byte("proof"), false)
	r, err := s.Record(ctx, in, []db.TrafficRef{{TrafficID: "v863-rm", Role: "proof"}})
	if err != nil {
		t.Fatal(err)
	}
	b := r.Traffic.Bindings[0]
	if err = s.DB.EditFindingTraffic(ctx, r.FindingID, b.ID, r.Traffic.Version, nil, nil, true, nil); err != nil {
		t.Fatal(err)
	}
	v, err := s.ValidateFindingEvidence(ctx, r.FindingID)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != EvidenceInvalid || v.Bindings != 0 {
		t.Fatalf("removed bindings=%+v", v)
	}
	if f, err := s.DB.GetFinding(r.FindingID); err != nil || f == nil {
		t.Fatalf("historical finding lost: %v", err)
	}
}

// TestEvidenceValidityNoEvidence_v863: a text-only finding is "none", not
// "invalid", and remains exportable.
func TestEvidenceValidityNoEvidence_v863(t *testing.T) {
	s, in, _ := evidenceFixture(t)
	ctx := context.Background()
	r, err := s.Record(ctx, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.ValidateFindingEvidence(ctx, r.FindingID)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != EvidenceNoEvidence || v.Bindings != 0 {
		t.Fatalf("text-only validity=%+v", v)
	}
	if err = s.StageFindingsExport(ctx, []*db.DBFinding{{ID: r.FindingID}}, t.TempDir(), false); err != nil {
		t.Fatalf("text-only export blocked: %v", err)
	}
}

// TestEvidenceProvenanceCallerCannotForge_v863 is the F-B1 app-level contract:
// a caller supplies only a traffic id/role/note. Content and hashes are read
// from the capture store by the server, the snapshot id is content-bound, and
// unknown traffic or a forged snapshot id is rejected without side effects.
func TestEvidenceProvenanceCallerCannotForge_v863(t *testing.T) {
	s, in, _ := evidenceFixture(t)
	ctx := context.Background()
	body := []byte("authorized capture body")
	seedExchange(t, s, "v863-prov", body, false)
	base, err := s.Record(ctx, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Bind(ctx, base.FindingID, []db.TrafficRef{{TrafficID: "v863-prov", Role: "proof"}})
	if err != nil {
		t.Fatal(err)
	}
	b := r.Bindings[0]
	sum := sha256.Sum256(body)
	if b.Snapshot.RespHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("response hash not derived from capture: %s", b.Snapshot.RespHash)
	}
	if b.Snapshot.ID != db.TrafficSnapshotID(b.Snapshot) {
		t.Fatal("snapshot id is not content-bound")
	}
	f, _, err := s.OpenBody(b.Snapshot, "response")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(f)
	f.Close()
	if err != nil || !strings.Contains(string(got), string(body)) {
		t.Fatalf("stored body mismatch: %q err=%v", got, err)
	}

	if _, err := s.Bind(ctx, base.FindingID, []db.TrafficRef{{TrafficID: "v863-absent"}}); err == nil {
		t.Fatal("unknown traffic id accepted")
	}
	after, err := s.DB.GetFindingTraffic(ctx, base.FindingID)
	if err != nil || len(after.Bindings) != 1 {
		t.Fatalf("rejected bind mutated state: %+v err=%v", after, err)
	}

	err = s.DB.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		bad := db.TrafficEvidenceSnapshot{
			ID: "forged", SourceTrafficID: "x", URL: "https://x/", Method: "GET",
			ReqHash: strings.Repeat("0", 64), RespHash: strings.Repeat("0", 64),
		}
		return db.InsertEvidenceSnapshotTx(tx, bad)
	})
	if err == nil {
		t.Fatal("forged snapshot id accepted")
	}

	if _, err := db.NormalizeTrafficRefs([]db.TrafficRef{{TrafficID: "   "}}); err == nil {
		t.Fatal("empty traffic id accepted")
	}
	if _, err := db.NormalizeTrafficRefs([]db.TrafficRef{{TrafficID: "x", Role: "owner"}}); err == nil {
		t.Fatal("unknown role accepted")
	}
}

// TestEvidenceValidityConcurrentBind_v863 uses a barrier (no sleeps) so the
// validator and binder contend for the same finding. Every observed state must
// be a complete valid state; a torn read or an error fails the test.
func TestEvidenceValidityConcurrentBind_v863(t *testing.T) {
	s, in, _ := evidenceFixture(t)
	ctx := context.Background()
	seedExchange(t, s, "v863-c1", []byte("first"), false)
	seedExchange(t, s, "v863-c2", []byte("second"), false)
	r, err := s.Record(ctx, in, []db.TrafficRef{{TrafficID: "v863-c1"}})
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 20; i++ {
			v, err := s.ValidateFindingEvidence(ctx, r.FindingID)
			if err != nil {
				errCh <- err
				return
			}
			if v.State != EvidenceValid {
				errCh <- fmt.Errorf("observed torn state: %+v", v)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 20; i++ {
			if _, err := s.Bind(ctx, r.FindingID, []db.TrafficRef{{TrafficID: "v863-c2"}}); err != nil {
				errCh <- err
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	final, err := s.ValidateFindingEvidence(ctx, r.FindingID)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != EvidenceValid || final.Bindings != 2 {
		t.Fatalf("final=%+v", final)
	}
}
