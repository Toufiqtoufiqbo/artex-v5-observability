// Package telemetry implements Phase IX.2.3 — execution observability &
// performance diagnostics. It provides:
//
//   - one structured event model (Event) with the B1 lifecycle (STARTED/COMPLETED/
//     FAILED/TIMED_OUT/CANCELLED/RETRY/SKIPPED/REJECTED);
//   - a bounded in-memory ring + append-only JSONL store (Recorder);
//   - a bounded metrics model (Registry/Metric) with documented definitions.
//
// Privacy: events never contain raw prompts/responses or secrets. Redaction is
// applied at the store boundary. Failures of telemetry never change execution.
package telemetry

import (
	"encoding/json"
	"sort"
	"time"
)

// EventType is the lifecycle type of one telemetry event.
type EventType string

// Contract (Gate B2): lifecycle event types.
const (
	EngineTaskStarted         EventType = "ENGINE_TASK_STARTED"
	EngineTaskCompleted       EventType = "ENGINE_TASK_COMPLETED"
	EngineTaskFailed          EventType = "ENGINE_TASK_FAILED"
	EngineTaskTimedOut        EventType = "ENGINE_TASK_TIMED_OUT"
	EngineTaskCancelled       EventType = "ENGINE_TASK_CANCELLED"
	PlannerWakeupStarted      EventType = "ENGINE_PLANNER_WAKEUP_STARTED"
	PlannerWakeupCompleted    EventType = "ENGINE_PLANNER_WAKEUP_COMPLETED"
	PlannerPlanStarted        EventType = "PLANNER_PLAN_STARTED"
	PlannerPlanCompleted      EventType = "PLANNER_PLAN_COMPLETED"
	PlannerPlanFailed         EventType = "PLANNER_PLAN_FAILED"
	PlannerGoalDecomposed     EventType = "PLANNER_GOAL_DECOMPOSED"
	PlannerIntentCreated      EventType = "PLANNER_INTENT_CREATED"
	PlannerIntentDuplicate    EventType = "PLANNER_INTENT_DUPLICATE_SUPPRESSED"
	PlannerPlanValidated      EventType = "PLANNER_PLAN_VALIDATED"
	WorkerExecutionStarted    EventType = "WORKER_EXECUTION_STARTED"
	WorkerExecutionCompleted  EventType = "WORKER_EXECUTION_COMPLETED"
	WorkerExecutionFailed     EventType = "WORKER_EXECUTION_FAILED"
	ToolInvoked               EventType = "TOOL_INVOKED"
	ToolCompleted             EventType = "TOOL_COMPLETED"
	ToolFailed                EventType = "TOOL_FAILED"
	ProviderCallStarted       EventType = "PROVIDER_CALL_STARTED"
	ProviderCallCompleted     EventType = "PROVIDER_CALL_COMPLETED"
	ProviderCallFailed        EventType = "PROVIDER_CALL_FAILED"
	ProviderRetry             EventType = "PROVIDER_RETRY"
	FindingSubmitted          EventType = "FINDING_SUBMITTED"
	FindingDuplicate          EventType = "FINDING_DUPLICATE"
	FindingPersisted          EventType = "FINDING_PERSISTED"
	EvidenceValidated         EventType = "EVIDENCE_VALIDATED"
)

// Outcome values used in events/metrics.
const (
	OutcomeOK        = "ok"
	OutcomeFailed    = "failed"
	OutcomeCancelled = "cancelled"
	OutcomeTimedOut  = "timed_out"
	OutcomeSkipped   = "skipped"
	OutcomeRejected  = "rejected"
)

// ErrorClass values (Gate D3). USAGE_UNKNOWN is the required token outcome.
const (
	ErrClassValidation         = "validation_error"
	ErrClassAuthorization      = "authorization_rejection"
	ErrClassToolUnavailable    = "tool_unavailable"
	ErrClassProvider           = "provider_failure"
	ErrClassTimeout            = "timeout"
	ErrClassCancellation       = "cancellation"
	ErrClassPersistence        = "persistence_failure"
	ErrClassEvidenceIntegrity  = "evidence_integrity_failure"
	ErrClassInternal           = "internal_execution_failure"
	ErrClassUnknown            = "unknown_failure"
	UsageUnknown               = "USAGE_UNKNOWN"
)

// UsageMetadata: distinguishes missing metadata from zero usage (Gate E2).
type UsageMetadata struct {
	Available bool `json:"available"`
	Input     *int `json:"input_tokens,omitempty"`
	Output    *int `json:"output_tokens,omitempty"`
}

// Event is the canonical execution event (Gate B1).
type Event struct {
	EventID        string            `json:"event_id"`
	EventType      EventType         `json:"event_type"`
	EventVersion   int               `json:"event_version"`
	RunID          string            `json:"run_id,omitempty"`
	TaskID         string            `json:"task_id,omitempty"`
	ParentTaskID   string            `json:"parent_task_id,omitempty"`
	ExecutionID    string            `json:"execution_id,omitempty"`
	Component      string            `json:"component"`
	Stage          string            `json:"stage,omitempty"`
	Operation      string            `json:"operation,omitempty"`
	CorrelationID  string            `json:"correlation_id,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	CompletedAt    *time.Time        `json:"completed_at,omitempty"`
	DurationMS     *int64            `json:"duration_ms,omitempty"`
	QueueWaitMS    *int64            `json:"queue_wait_ms,omitempty"`
	Outcome        string            `json:"outcome,omitempty"`
	ErrorClass     string            `json:"error_class,omitempty"`
	RetryNumber    int               `json:"retry_number,omitempty"`
	Provider       string            `json:"provider,omitempty"`
	Model          string            `json:"model,omitempty"`
	UsageMetadata  UsageMetadata     `json:"usage_metadata"`
	Metrics        map[string]any    `json:"metrics,omitempty"`
}

const eventVersion = 1

// Redact bounds stored strings to 512 chars. Applied on store boundary (B6).
func Redact(s string) string {
	const maxLen = 512
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// NewEvent creates a v1 event with a new time-based id (Gate B1).
func NewEvent(eventType EventType, component string, taskID string) *Event {
	return &Event{
		EventID:      NewEventID(),
		EventType:    eventType,
		EventVersion: eventVersion,
		Component:    component,
		TaskID:       Redact(taskID),
		StartedAt:    time.Now().UTC(),
	}
}

// NewEventID returns a sortable unique id (time-based).
func NewEventID() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + randSuffix()
}

func randSuffix() string {
	var b [4]byte
	// math/rand without seeding (deterministic-safe, only for uniqueness).
	x := uint32(time.Now().UnixNano())
	for i := range b {
		x = x*1664525 + 1013904223
		b[i] = byte(x >> 24)
	}
	return hex(b[:])
}

func hex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0xf])
	}
	return string(out)
}

// End sets the completion timestamp and duration. Returns the event for chaining.
func (e *Event) End(outcome string, errClass string) *Event {
	now := time.Now().UTC()
	e.CompletedAt = &now
	if e.DurationMS == nil {
		ms := now.Sub(e.StartedAt).Milliseconds()
		e.DurationMS = &ms
	}
	e.Outcome = outcome
	e.ErrorClass = errClass
	return e
}

// MarshalJSON guards against leaking secrets: redacts Component/Operation.
func (e *Event) MarshalJSON() ([]byte, error) {
	type alias Event
	a := alias(*e)
	a.Component = Redact(a.Component)
	a.Operation = Redact(a.Operation)
	if a.Metrics != nil {
		a.Metrics = map[string]any{}
		for k, v := range e.Metrics {
			switch v := v.(type) {
			case string:
				a.Metrics[k] = Redact(v)
			case bool, int, int64, float64, nil:
				a.Metrics[k] = v
			default:
				a.Metrics[k] = Redact(toString(v))
			}
		}
	}
	return json.Marshal(a)
}

func toString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Compare returns true if e and other have the same correlation id and are
// different events (used by tests).
func (e *Event) SameCorrelation(other *Event) bool {
	return e.CorrelationID != "" && e.CorrelationID == other.CorrelationID
}

// SortEvents sorts events by event id (chronological).
func SortEvents(evs []*Event) {
	sort.Slice(evs, func(i, j int) bool { return evs[i].EventID < evs[j].EventID })
}
