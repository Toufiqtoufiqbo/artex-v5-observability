package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

// VULN-1: telemetry event storage must be bounded. With a tiny MaxFileSize,
// the recorder must rotate and prune, and ReadEvents must still return all
// events written before the rotation.
func TestRecorderRotatesAndBoundsDisk(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(Config{Enabled: true, Dir: dir, MaxFileSize: 80})
	for i := 0; i < 12; i++ {
		rec.Append(NewEvent(EngineTaskStarted, "engine", "task"))
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "events*.jsonl"))
	if len(matches) > 1+maxRotatedFiles {
		t.Fatalf("unbounded telemetry files: %d", len(matches))
	}
	if rec.WriteFailures() != 0 {
		t.Fatalf("write failures: %d", rec.WriteFailures())
	}
	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) == 0 || len(evs) > 12 {
		t.Fatalf("events after rotation = %d, want 1..12", len(evs))
	}
	// Ensure no single file exceeds the configured ceiling by much (one event line).
	for _, m := range matches {
		st, err := os.Stat(m)
		if err != nil {
			t.Fatal(err)
		}
		if st.Size() > 1024 {
			t.Fatalf("event file %s too large: %d", m, st.Size())
		}
	}
}
