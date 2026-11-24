# EXECUTION TELEMETRY — DATA CONTRACT (Gate B, IX.2.3.1)

**Generated:** 2026-11-24
**Phase:** IX.2.3.1 — Timeline Attribution + Tool Duration Aggregation + Diagnostics API
**Primary files:** `telemetry/timeline.go`, `telemetry/metrics.go` (+ tool metrics), `db/commands.go`, `server/diagnostics.go`
**Supersedes:** `EXECUTION_TELEMETRY_SCHEMA_SPECIFICATION.md` (IX.2.3 v1). This document adds the deltas.
**Evidence labels:** `SOURCE VERIFIED`, `CALL-PATH VERIFIED`, `TEST VERIFIED`, `INTEGRATION VERIFIED`, `RUNTIME VERIFIED`, `PARTIALLY VERIFIED`, `UNKNOWN`, `NOT INVESTIGATED`.

---

## B1. Timeline (new in IX.2.3.1)

`telemetry/timeline.go` — `BuildTimeline(evs []*Event, taskID string) Timeline` (H2, TEST VERIFIED by telemetry/timeline_test.go).

- `Timeline.Flat` — all events for the task sorted by `StartedAt` (B3).
- `Timeline.Root` — tree grouped by nesting heuristics: `EngineTaskStarted`/`WorkerExecutionStarted`/`PlannerPlanStarted` push children; completions pop.
- `Timeline.Durations()` — `(totals map[string]uint64, counts map[string]uint64)` keyed by component(+stage).
- `Timeline.String()` — human-readable timeline.

Bounded: one task per timeline; use `ReadEvents` + filters for full graphs (§14).

## B2. Tool duration aggregation (new in IX.2.3.1)

`telemetry/metrics.go` — `EmitToolDuration(category string, durationMS int64, failed bool)`:

- Metrics: `tool.duration_ms[read|write|shell|web|other]` (Histogram), `tool.failures{category}` (Counter), `tool.invocations` (Counter).
- Categories bounded to the enum (§D2).

Instrumented call path: `db.ListCommands` (`db/commands.go`) — emits tool duration from existing tool_use→tool_result timestamps. Zero when no result row exists yet (missing telemetry ≠ 0, §17).

Test: `db/commands_telemetry_test.go` — `TestToolCategoryForTelemetryBounded` (TEST VERIFIED).

## B3. Diagnostics API (Gate I2, TEST VERIFIED by server build + smoke)

| Route | Handler | Returns |
|---|---|---|
| `GET /api/diagnostics/summary` | `server/diagnostics.go` | Metrics snapshot, events total, write failures |
| `GET /api/diagnostics/run?run_id=` | `server/diagnostics.go` | Filtered events |
| `GET /api/diagnostics/task?task_id=` | `server/diagnostics.go` | Filtered events |
| `GET /api/diagnostics/timeline?task_id=` | `server/diagnostics.go` | `telemetry.BuildTimeline` |

Privacy recap: bounded categories only, no prompts in events (§B4).

## B4. Evidence (IX.2.3.1)

| Claim | Classification | Evidence |
|---|---|---|
| Timeline nests events per task | TEST VERIFIED | `telemetry/timeline_test.go` (3 tests) |
| Tool durations flow from DB | CALL-PATH VERIFIED | `db/commands.go:ListCommands` emit loop |
| Diagnostics routes registered | SOURCE VERIFIED | `server/server.go` + `server/diagnostics.go` |
| No privacy regression | TEST VERIFIED | `telemetry/telemetry_test.go` |
