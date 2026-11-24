# PHASE IX.2.3 — EXECUTION OBSERVABILITY & PERFORMANCE DIAGNOSTICS
## Gate A — Existing Observability Forensic Audit

**Generated:** 2026-11-24
**HEAD:** `e6ec56999175509551a1e43a3ee3b053d0941bd9`
**Working tree recorded before edits:** 112 entries (`$env:TEMP\v923_git_status.txt`).
**Scope:** read-only. No application source modified during this gate.
**Evidence labels:** `SOURCE VERIFIED`, `CALL-PATH VERIFIED`, `TEST VERIFIED`, `INTEGRATION VERIFIED`, `RUNTIME VERIFIED`, `PARTIALLY VERIFIED`, `UNKNOWN`, `NOT INVESTIGATED`.

---

## A1. Inventory of existing telemetry (revalidated against current HEAD)

| Capability | Status | Evidence |
|---|---|---|
| Task lifecycle (status, reason) | EXISTS AND INTEGRATED | `db/tasks.go`, `server/manager.go` (`SetTaskStatus`) |
| Engine activity ledger | EXISTS AND INTEGRATED | `db.Activity` (`db/exploration.go:87`), `ActivityList` (`:1496`), emitted by Engine/Worker (`server/engine.go:651`) |
| Tool invocation records (tool_use/tool_result) | EXISTS AND INTEGRATED | `db.ListCommands` (`db/commands.go:83`) — timestamps of each tool call (usable as durations) |
| LLM usage ledger (tokens, profile, status, latency_ms) | EXISTS AND INTEGRATED | `db.LLMUsage` (`db/llm_usage.go:15-27`), `InsertLLMUsage` (`:68`), written by `llmrec` |
| Guard audit log | EXISTS AND INTEGRATED | `guard/guard.go:184` (`AuditEntry`) |
| Task timeout/deadline coordinator | EXISTS AND INTEGRATED | `server/engine_timeout.go`, `startDeadlineCoordinator` (`server/engine.go:779`) |
| Finding dedup counters | EXISTS AND INTEGRATED (IX.2.1) | `db/finding_dedup_telemetry.go` |
| Benchmark timing fields (planner/worker/tool/evidence) | EXISTS AND INTEGRATED (IX.1) | `benchmark/runner.go:146,160,276`, `CaseMetrics.Timings` |
| `RACE DETECTOR UNKNOWN` | **UNRESOLVED — preserved (§A2.10)** |

## A2. Instrumentation gaps (all PRODUCTION, runtime-relevant)

### A2.1 Missing lifecycle events
- No structured STARTED/COMPLETED events — only free-form activity rows (`Activity.Kind` is an enum per producer). **NOT INVESTIGATED**
- Engine has no "task started" event beyond `touch(t.ID)` (in-memory only). **NOT INVESTIGATED**
- Worker has no duration measurement. **NOT INVESTIGATED**
- Planner rounds lack end-to-end duration (benchmark has `PlannerModelTime` but runtime has none). **NOT INVESTIGATED**

### A2.2 Missing duration measurements
- Planning duration: not measured at runtime (benchmark only). **NOT INVESTIGATED**
- Worker duration: not measured (benchmark only). **NOT INVESTIGATED**
- Tool duration: data exists (tool_use/tool_result timestamps via `ListCommands`) but **no metrics/aggregation**. **PARTIALLY VERIFIED**
- Evidence validation duration: benchmark-only (`evidence_validation_time`). **NOT INVESTIGATED**
- Database persistence timing: not measured. **NOT INVESTIGATED**

### A2.3 Fields that are calculated but never persisted
- `Activity.CreatedAt` deltas ≈ stage durations, but never materialized as fields. **SOURCE VERIFIED**

### A2.4 Counters with inconsistent updates
- `llm_usage` writes one row per call (good); there is no retry metric at runtime. **NOT INVESTIGATED**

### A2.5 Always-zero fields due to missing wiring
- `LLMUsage.LatencyMs` is populated only in the connection test (`agent/provider.go:542`), not in live sessions (llmrec passes it, but no one else measures it). **SOURCE VERIFIED**

### A2.6 Lost correlation identifiers
- `activity.ToolUseID` links tool_use→result (good). No `execution_id` across planner/worker. **NOT INVESTIGATED**

### A2.7 Duplicate logging
- Engine already logs terminal states via `log.Printf`; adding telemetry events is additive. **PARTIALLY VERIFIED**

### A2.8 High-cardinality metrics risk
- To be controlled with bounded label enums (§Gate B). **NOT INVESTIGATED**

### A2.9 Mock-mode telemetry mixing risk
- `ScriptedProvider` is benchmark-only; live mode unaffected. Runtime metric `llm_usage.profile_name` exists for disambiguation. **PARTIALLY VERIFIED**

### A2.10 RACE DETECTOR UNKNOWN — ENVIRONMENT BLOCKED
Preserved: `RACE DETECTOR UNKNOWN — ENVIRONMENT BLOCKED` remains unresolved (no race-detector runs were enabled in this environment).

## A3. Execution lifecycle diagram (existing + planned instrumentation)

```
Request → Engine.Run → plannerLoop → Planner.Plan
                                                        │ PLANNER_PLAN_STARTED/COMPLETED
                                                        ▼
                                          Intent Management (add_intent)
                                                        │ PLANNER_INTENT_CREATED/DUPLICATE_SUPPRESSED
                                                        ▼
                                              Frontier → claimNext → runIntent
                                                        │ WORKER_EXECUTION_STARTED/COMPLETED
                                                        ▼
                                          Worker → tools → LLM (llmrec → PROVIDER_CALL_*)
                                                        │ TOOL_*
                                                        ▼
                                          Evidence → ValidateFindingEvidence → EVIDENCE_VALIDATED
                                                        ▼
                                          Finding → RecordFindingTx → FINDING_SUBMITTED/PERSISTED
                                                        ▼
                                               Task Settlement → ENGINE_TASK_COMPLETED/FAILED
```

## A4. Recommendation for Gate B

Implement `telemetry/` (event model + metrics + store) and instrument at the call sites in Gates C–H of this report. Reuse `db.LLMUsage` and `Activity` rather than new storage.
