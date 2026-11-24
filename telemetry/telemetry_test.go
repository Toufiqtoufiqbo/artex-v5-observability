package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventRedactBoundsStoredStrings(t *testing.T) {
	ev := NewEvent(EngineTaskStarted, "engine", "task-1")
	long := strings.Repeat("x", 4096)
	ev.Operation = long
	if len(ev.Operation) != 4096 {
		t.Fatalf("Redact does not bound strings")
	}
	b, err := ev.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	// MarshalJSON redacts: the marshalled JSON must be small.
	if len(b) > 4100 {
		t.Fatalf("marshalled event too large: %d", len(b))
	}
	if strings.Contains(string(b), strings.Repeat("x", 4000)) {
		t.Fatal("event leaks unbounded strings")
	}
	got := Redact("abc")
	if got != "abc" {
		t.Fatalf("short string redacted: %q", got)
	}
}

func TestEventLifecycleAndDuration(t *testing.T) {
	ev := NewEvent(EngineTaskStarted, "engine", "task-1")
	if ev.EventType != EngineTaskStarted || ev.EventVersion != 1 || ev.TaskID != "task-1" {
		t.Fatalf("new event fields: %+v", ev)
	}
	if ev.EventID == "" || !strings.Contains(ev.EventID, "T") {
		t.Fatalf("event id not sortable: %q", ev.EventID)
	}
	ev.End(OutcomeOK, "")
	if ev.Outcome != OutcomeOK || ev.DurationMS == nil || *ev.DurationMS < 0 || ev.CompletedAt == nil {
		t.Fatalf("End() did not set outcome/duration/completed_at: %+v", ev)
	}
	SortEvents([]*Event{ev, NewEvent(EngineTaskCompleted, "engine", "t2")})
}

func TestUsageUnknownDistinctFromZero(t *testing.T) {
	ev := NewEvent(ProviderCallCompleted, "provider", "task-1")
	ev.UsageMetadata = UsageMetadata{Available: false}
	b, _ := ev.MarshalJSON()
	if !strings.Contains(string(b), `"available":false`) {
		t.Fatalf("missing usage metadata must be visible as not-available, got %s", string(b))
	}
	if strings.Contains(string(b), `"input_tokens":0`) || strings.Contains(string(b), `"output_tokens":0`) {
		t.Fatal("missing usage must not be encoded as literal zero tokens")
	}
}

func TestRecorderAppendReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(Config{Enabled: true, Dir: dir})
	ev := NewEvent(EngineTaskStarted, "engine", "task-1")
	ev.End(OutcomeOK, "")
	rec.Append(ev)
	if rec.WriteFailures() != 0 {
		t.Fatalf("write failures: %d", rec.WriteFailures())
	}
	evs, err := ReadEvents(dir)
	if err != nil || len(evs) != 1 {
		t.Fatalf("ReadEvents: n=%d err=%v", len(evs), err)
	}
	if evs[0].EventType != EngineTaskStarted || evs[0].TaskID != "task-1" {
		t.Fatalf("round trip mismatch: %+v", evs[0])
	}
	if evs[0].EventID != ev.EventID {
		t.Fatalf("event id mismatch")
	}
	rec.Close()
}

func TestRecorderReadMissingDirIsNotError(t *testing.T) {
	evs, err := ReadEvents(filepath.Join(t.TempDir(), "nope"))
	if err != nil || evs != nil {
		t.Fatalf("missing store should be empty, err=%v n=%d", err, len(evs))
	}
}

func TestRecorderDisabledIsNoOp(t *testing.T) {
	rec := NewRecorder(Config{Enabled: false, Dir: t.TempDir()})
	rec.Append(NewEvent(EngineTaskStarted, "engine", "t"))
	if rec.EventsInStore() != 0 {
		t.Fatal("disabled recorder must not store")
	}
}

func TestRegistryMetrics(t *testing.T) {
	r := NewRegistry()
	defs := r.AllMetricDefs()
	if len(defs) == 0 {
		t.Fatal("registry must define documented metrics")
	}
	for _, m := range defs {
		if m.Help == "" {
			t.Fatalf("metric %s missing documented help", m.Name)
		}
	}
	r.Inc("engine.tasks.completed", map[string]string{"outcome": OutcomeOK}, 1)
	r.Observe("engine.tasks.duration_ms", 42)
	snap := r.Snapshot()
	if snap["engine.tasks.completed"].Value != 1 {
		t.Fatalf("counter = %d, want 1", snap["engine.tasks.completed"].Value)
	}
	if snap["engine.tasks.duration_ms"].Percentiles == nil || snap["engine.tasks.duration_ms"].Percentiles["max"] != 42 {
		t.Fatalf("histogram percentiles missing: %+v", snap["engine.tasks.duration_ms"])
	}
}

func TestRegistryNilSafe(t *testing.T) {
	var r *Registry
	r.Inc("engine.tasks.completed", nil, 1)
	r.Observe("engine.tasks.duration_ms", 1)
	if len(r.Snapshot()) != 0 {
		t.Fatal("nil registry snapshot must be empty")
	}
}

func TestSnapshotToFile(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry()
	r.Inc("engine.tasks.completed", nil, 1)
	p, err := SnapshotToFile(r, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}
}
