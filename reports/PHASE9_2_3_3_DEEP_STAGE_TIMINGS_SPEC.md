# PHASE IX.2.3.3 — DEEP STAGE TIMINGS (Gate C extension)

**Generated:** 2026-11-24
**Evidence labels:** `SOURCE VERIFIED`, `CALL-PATH VERIFIED`, `TEST VERIFIED`, `INTEGRATION VERIFIED`, `RUNTIME VERIFIED`, `PARTIALLY VERIFIED`, `UNKNOWN`, `NOT INVESTIGATED`.

---

## C1. Goal decomposition (agent/goals.go:110)

- `DecomposeGoalsWithProvider` timed around `captureRun` (start at entry, stop after).
- Emits `PLANNER_GOAL_DECOMPOSED` (§B1, agent/goals.go).
- Metric: `planner.goal_decompose_ms` (Histogram, documented).

Evidence: source (agent/goals.go:153-165), call-path (server/goals.go:387).

## C2. Scope (files changed)

- Added: `agent/goals.go` timing + event.
- Modified: `telemetry/metrics.go` (metric added).

## C3. Validation

| Step | Result |
|---|---|
| `go build ./...` | 0 |
| `go vet ./agent ./telemetry` | 0 |
| `go test ./telemetry -count=1` | ok |

## C4. Limitations

- `planner.goal_decompose_ms` measures the session, not the first-token latency (§E).
