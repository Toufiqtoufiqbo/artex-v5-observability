# EXECUTION TELEMETRY — DATA CONTRACT (Gate B, IX.2.3)

**Generated:** 2026-11-24
**Phase:** IX.2.3 — Execution Observability & Performance Diagnostics
**Primary files:** `telemetry/event.go`, `telemetry/metrics.go`, `telemetry/store.go`
**Deltas in IX.2.3.1:** timeline + tool durations + diagnostics API — see `EXECUTION_TELEMETRY_SCHEMA_SPECIFICATION_IX231.md`.
**Evidence labels:** `SOURCE VERIFIED`, `CALL-PATH VERIFIED`, `TEST VERIFIED`, `INTEGRATION VERIFIED`, `RUNTIME VERIFIED`, `PARTIALLY VERIFIED`, `UNKNOWN`, `NOT INVESTIGATED`.

---

## B1. Event model (Gate B1 — TEST VERIFIED by telemetry/telemetry_test.go)

Fields (`telemetry/event.go`): `event_id`, `event_type`, `event_version`, `run_id`, `task_id`, `parent_task_id`, `execution_id`, `component`, `stage`, `operation`, `correlation_id`, `started_at`, `completed_at`, `duration_ms`, `queue_wait_ms`, `outcome`, `error_class`, `retry_number`, `provider`, `model`, `usage_metadata{available,input_tokens,output_tokens}`, `metrics`.

## B2. Lifecycle (Gate B2 — TEST VERIFIED)

`ENGINE_TASK_STARTED/COMPLETED/FAILED/TIMED_OUT/CANCELLED`, `PLANNER_PLAN_STARTED/_COMPLETED/_FAILED`, `PLANNER_INTENT_CREATED`, `PLANNER_INTENT_DUPLICATE_SUPPRESSED`, `WORKER_EXECUTION_STARTED/_COMPLETED/_FAILED`, `TOOL_INVOKED/TOOL_COMPLETED/TOOL_FAILED`, `PROVIDER_CALL_STARTED/_COMPLETED/_FAILED`, `PROVIDER_RETRY`, `FINDING_SUBMITTED/FINDING_DUPLICATE/FINDING_PERSISTED`, `EVIDENCE_VALIDATED`.

## B3. Timestamps & durations (Gate B3 — TEST VERIFIED)

UTC for persistence; monotonic `time.Since` inside a process; `queue_wait_ms` (nil when not measurable).

## B4. Correlation (Gate B4 — TEST VERIFIED)

`task_id` → run → planner → worker → tool → provider → finding → evidence. Retries share execution id, increment `retry_number`.

## B5. Storage & reliability (Gate B4 — TEST VERIFIED)

Append-only JSONL at `<dir>/events.jsonl`; malformed lines skipped; write failures counted; never fails execution.

## B6. Privacy (Gates D4/E4/K11/K12 — TEST VERIFIED)

`Redact()` (512 chars); no prompts/responses; `usage_metadata.available=false` for missing usage (§E2).

## B7. Metrics (Gate G2 — TEST VERIFIED)

`telemetry/metrics.go` — 29 documented metrics (G2 table). Cardinality-safe labels (§G3).

## B8. Diagnostics interfaces (Gate I4)

`artex diagnostics summary|run|task|metrics` (IX.2.3); extended by IX.2.3.1 API — see the IX231 spec.
