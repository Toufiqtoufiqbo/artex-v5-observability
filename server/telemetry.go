package server

import (
	"time"

	"github.com/Autumn-27/artex/telemetry"
)

// telemetry.go — Phase IX.2.3 (Gates C/H): engine-side execution observability.
// Instrumentation is added at real production call sites; the telemetry layer
// itself is in package telemetry (see telemetry/event.go, telemetry/metrics.go).
// All helpers are nil-safe: disabled telemetry adds ~0 overhead.

func emitEngineTaskStarted(t *Task) {
	ev := telemetry.NewEvent(telemetry.EngineTaskStarted, "engine", t.ID)
	telemetry.Emit(ev)
}

// emitEngineTaskSettled reports task terminal outcome (completed/failed/cancelled/timed_out).
func emitEngineTaskSettled(t *Task, outcome, errClass string) {
	ev := telemetry.NewEvent(telemetry.EngineTaskCompleted, "engine", t.ID)
	switch outcome {
	case "failed":
		ev.EventType = telemetry.EngineTaskFailed
	case "cancelled":
		ev.EventType = telemetry.EngineTaskCancelled
	case "timed_out":
		ev.EventType = telemetry.EngineTaskTimedOut
	}
	ev.Outcome = outcome
	ev.ErrorClass = errClass
	telemetry.Emit(ev)
	telemetry.DefaultRegistry().Inc("engine.tasks."+map[string]string{
		"completed":  "completed",
		"failed":     "failed",
		"cancelled":  "cancelled",
		"timed_out":  "timed_out",
	}[outcome], nil, 1)
}

func emitPlannerWakeupStarted(t *Task) *telemetry.Event {
	ev := telemetry.NewEvent(telemetry.PlannerWakeupStarted, "engine", t.ID)
	telemetry.Emit(ev)
	return ev
}

func emitPlannerWakeupCompleted(t *Task, start time.Time) {
	ev := telemetry.NewEvent(telemetry.PlannerWakeupCompleted, "engine", t.ID)
	ev.DurationMS = msPtr(time.Since(start))
	telemetry.Emit(ev)
}

func emitWorkerExecutionStarted(t *Task, intentID int64, worker string) *telemetry.Event {
	ev := telemetry.NewEvent(telemetry.WorkerExecutionStarted, "worker", t.ID)
	ev.Stage = "execute"
	ev.Operation = worker
	ev.Metrics = map[string]any{"intent_id": intentID}
	telemetry.Emit(ev)
	return ev
}

func emitWorkerExecutionFinished(t *Task, ev *telemetry.Event, outcome, errClass string, duration time.Duration) {
	if ev == nil {
		return
	}
	ev2 := telemetry.NewEvent(telemetry.WorkerExecutionCompleted, "worker", t.ID)
	ev2.Stage = "execute"
	ev2.Operation = ev.Operation
	ev2.Outcome = outcome
	ev2.ErrorClass = errClass
	ev2.DurationMS = msPtr(duration)
	ev2.Metrics = ev.Metrics
	telemetry.Emit(ev2)
	telemetry.DefaultRegistry().Observe("worker.duration_ms", uint64(duration.Milliseconds()))
	telemetry.DefaultRegistry().Inc("worker.executions", map[string]string{"outcome": outcome}, 1)
	if errClass != "" {
		telemetry.DefaultRegistry().Inc("worker.execution_errors", nil, 1)
	}
}

func msPtr(d time.Duration) *int64 {
	ms := d.Milliseconds()
	return &ms
}
