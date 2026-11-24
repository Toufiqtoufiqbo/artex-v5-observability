package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/telemetry"
	"github.com/Autumn-27/norma/llm"
)

// Regression for global re-audit F1: one Planner.Plan round must emit exactly
// one PLANNER_PLAN_COMPLETED event and must NOT emit PLANNER_GOAL_DECOMPOSED
// (that event belongs to DecomposeGoalsWithProvider in agent/goals.go).
func TestPlannerPlanEventsAreNotDuplicated_v923(t *testing.T) {
	d := testDB(t)
	defer d.Close()
	task, err := d.CreateTask("[v923] telemetry dedup", "test goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)

	dir := t.TempDir()
	rec := telemetry.NewRecorder(telemetry.Config{Enabled: true, Dir: dir})
	telemetry.SetDefaultRecorder(rec)
	defer func() {
		telemetry.SetDefaultRecorder(nil)
		_ = rec.Close()
	}()

	turn := 0
	provider := captureUsageProvider{stream: func(_ context.Context, yield func(llm.StreamEvent, error) bool) {
		turn++
		events := []llm.StreamEvent{{Type: llm.SETextDelta, Text: "done"}, {Type: llm.SEMessageDelta, StopReason: "end_turn"}}
		if turn == 1 {
			events = []llm.StreamEvent{
				{Type: llm.SEToolUseStart, ToolID: "i1", ToolName: "add_intent"},
				{Type: llm.SEToolInputJSON, Text: `{"summary":"唯一方向"}`},
				{Type: llm.SEMessageDelta, StopReason: "tool_use"},
			}
		}
		for _, ev := range events {
			if !yield(ev, nil) {
				return
			}
		}
	}}
	pl := NewPlanner(provider, "test-model", t.TempDir(), nil, 0, 6)
	if _, _, err := pl.Plan(context.Background(), task.ID, d.Assets(), d.Exploration(task.ExplorationID), "goal", nil, nil); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	evs, err := telemetry.ReadEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	completed, goalDecomposed := 0, 0
	for _, ev := range evs {
		t.Logf("event type=%s task=%s", ev.EventType, ev.TaskID)
		switch ev.EventType {
		case telemetry.PlannerPlanCompleted:
			completed++
		case telemetry.PlannerGoalDecomposed:
			goalDecomposed++
		}
	}
	if completed != 1 {
		t.Fatalf("PLANNER_PLAN_COMPLETED emitted %d times, want 1", completed)
	}
	if goalDecomposed != 0 {
		t.Fatalf("PLANNER_GOAL_DECOMPOSED emitted %d times from Plan(), want 0", goalDecomposed)
	}
	if len(evs) == 0 {
		t.Fatal("no telemetry events recorded")
	}
	_ = fmt.Sprint // keep import stable
}

var _ = db.KindIntent
