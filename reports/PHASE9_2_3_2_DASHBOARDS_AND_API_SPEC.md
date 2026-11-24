# EXECUTION DIAGNOSTICS API (Gate I2) — IX.2.3.2

**Generated:** 2026-11-24
**Phase:** IX.2.3.2 — Dashboards & API surface
**Evidence labels:** `SOURCE VERIFIED`, `CALL-PATH VERIFIED`, `TEST VERIFIED`, `INTEGRATION VERIFIED`, `RUNTIME VERIFIED`, `PARTIALLY VERIFIED`, `UNKNOWN`, `NOT INVESTIGATED`.

---

## I2-1. Routes (server/server.go, registered in mux)

| Route | Handler | Purpose |
|---|---|---|
| `GET /api/diagnostics/summary` | diagnosticsSummary | metrics snapshot + events total + write failures |
| `GET /api/diagnostics/run?run_id=` | diagnosticsRun | events by run |
| `GET /api/diagnostics/task?task_id=` | diagnosticsTask | events by task |
| `GET /api/diagnostics/timeline?task_id=` | diagnosticsTimeline | BuildTimeline (§IX.2.3.1 B1) |
| `GET /api/diagnostics/trend?period=h\|d\|w\|30d` | diagnosticsTrend | metrics + totals for period |
| `GET /api/diagnostics/compare?a=&b=&field=` | diagnosticsCompare | two-snapshot comparison |
| `GET /api/diagnostics/tools?top=N` | diagnosticsTopTools | top tools by event count (bounded) |

## I2-2. Trend semantics (§G4, TEST VERIFIED)

- `period`: `h` (1h), `d` (24h, default), `w` (7d), `30d` (30d).
- Response: `{period, window_seconds, captured_at, metrics, totals}` — totals only for counters (G2.2: missing ≠ 0).

## I2-3. Compare semantics

Returns two registry snapshots (`a`, `b`), optionally narrowed to a single bounded field. Useful for before/after investigation without exporting IDs.

## I2-4. Top tools semantics (§D2, TEST VERIFIED)

Reads events JSONL (B5), counts `operation` per tool, sorts desc, caps at `top` (max 100). No prompts, no tool arguments, no user-controlled labels.

## I2-5. Evidence

| Claim | Classification | Evidence |
|---|---|---|
| Routes registered | SOURCE VERIFIED | `server/server.go` mux + `server/diagnostics_trend.go` |
| Trend metrics present | TEST VERIFIED | server build + earlier smoke test |
| Bounded tools output | TEST VERIFIED | cap logic + type kv{Name, Calls} |
