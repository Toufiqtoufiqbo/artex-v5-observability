package telemetry

import (
	"testing"
)

func TestBuildTimelineNestsAndSorts(t *testing.T) {
	e1 := NewEvent(EngineTaskStarted, "engine", "t1")
	e2 := NewEvent(WorkerExecutionStarted, "worker", "t1")
	e3 := NewEvent(PlannerPlanStarted, "planner", "t1")
	e1.End(OutcomeOK, "")
	e3.End(OutcomeOK, "")
	e2.End(OutcomeOK, "")
	tl := BuildTimeline([]*Event{e2, e1, e3}, "t1")
	if len(tl.Flat) != 3 {
		t.Fatalf("flat=%d", len(tl.Flat))
	}
	tl.SortFlat()
	if len(tl.Flat) != 3 {
		t.Fatalf("flat=%d", len(tl.Flat))
	}
	for i := 1; i < len(tl.Flat); i++ {
		if tl.Flat[i-1].StartedAt.After(tl.Flat[i].StartedAt) {
			t.Fatalf("timeline not sorted: %v", tl.Flat)
		}
	}
	seen := map[string]bool{}
	for _, e := range tl.Flat {
		seen[string(e.EventType)] = true
	}
	if len(seen) != 3 || !seen[string(EngineTaskStarted)] {
		t.Fatalf("flat set = %v", seen)
	}
	if len(tl.Root.Children) == 0 || len(tl.Root.Children[0].Children) == 0 {
		t.Fatal("timeline nesting empty")
	}
	totals, counts := tl.Durations()
	if counts["engine:0"]+counts["engine"] != 1 {
		t.Fatalf("counts=%v", counts)
	}
	if _, ok := totals["engine"]; !ok {
		t.Fatal("totals missing engine")
	}
}

func TestBuildTimelineIgnoresOtherTasks(t *testing.T) {
	e1 := NewEvent(EngineTaskStarted, "engine", "t1")
	e2 := NewEvent(WorkerExecutionStarted, "worker", "t2")
	tl := BuildTimeline([]*Event{e1, e2}, "t1")
	if len(tl.Flat) != 1 || tl.Flat[0].EventType != EngineTaskStarted {
		t.Fatalf("tl=%+v", tl.Flat)
	}
}

func TestTimelineStringIsNonEmpty(t *testing.T) {
	e1 := NewEvent(EngineTaskStarted, "engine", "t1")
	e1.End(OutcomeOK, "")
	if s := BuildTimeline([]*Event{e1}, "t1").String(); s == "" {
		t.Fatal("empty timeline string")
	}
}
