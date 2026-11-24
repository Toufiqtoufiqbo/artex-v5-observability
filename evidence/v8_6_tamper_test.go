package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// VIII.6 Gate D — evidence tamper detection (integration; synthetic data).

func TestEvidenceTamperDetection_v86(t *testing.T) {
	s, _, _ := evidenceFixture(t)
	defer s.DB.Close()
	body := []byte("V8.6 synthetic evidence content for tamper tests (unique-v86)")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	if _, err := s.writeBody(bytes.NewReader(body), int64(len(body)), hash); err != nil {
		t.Fatalf("writeBody: %v", err)
	}
	path, err := hashPath(s.Dir, hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("blob missing at %s: %v", path, err)
	}
	if err := verifyFile(path, hash, int64(len(body))); err != nil {
		t.Fatalf("verifyFile intact: %v", err)
	}
	// Tamper #1: modify blob content → verifyFile must fail.
	if err := os.WriteFile(path, append(body, []byte("tampered")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(path, hash, int64(len(body))); err == nil {
		t.Fatal("verifyFile must fail on tampered content (F-D1)")
	}
	// Tamper #2: delete blob → verifyFile must fail.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(path, hash, int64(len(body))); err == nil {
		t.Fatal("verifyFile must fail on missing blob")
	}
}

func TestEvidenceOpenBodyRetrievalDetectsTamper_v86(t *testing.T) {
	s, _, _ := evidenceFixture(t)
	defer s.DB.Close()
	body := []byte("V8.6 synthetic evidence for OpenBody tamper test (unique-v86)")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	if _, err := s.writeBody(bytes.NewReader(body), int64(len(body)), hash); err != nil {
		t.Fatalf("writeBody: %v", err)
	}
	path, err := hashPath(s.Dir, hash)
	if err != nil {
		t.Fatal(err)
	}
	snap := db.TrafficEvidenceSnapshot{ID: "v86-snap", SourceTrafficID: "v86-tamper", ReqHash: hash, ReqLen: int64(len(body))}
	f, _, err := s.OpenBody(snap, "request")
	if err != nil {
		t.Fatalf("OpenBody intact: %v", err)
	}
	_ = f.Close()
	if err := os.WriteFile(path, append(body, []byte("tampered")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OpenBody(snap, "request"); err == nil {
		t.Fatal("OpenBody must detect tampered blob (retrieval-time verification, F-D1)")
	}
	// Invalid side must fail closed.
	if _, _, err := s.OpenBody(snap, "invalid-side"); err == nil {
		t.Fatal("OpenBody must reject invalid side")
	}
}

func TestEvidenceFixtureRequiresDSN_v86(t *testing.T) {
	t.Setenv("ARTEX_PG_DSN", "")
	s, _, _ := evidenceFixture(t)
	if s == nil {
		t.Log("skipped (no DSN)")
	}
}
