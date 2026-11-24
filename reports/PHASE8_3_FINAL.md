# Phase VIII.3 — FINAL

**Verdict: `PARTIAL`**

## Completed
- Phase 1: archive identity verified — SHA-256 `93AFBEC089A9D8AB00AE1DA4918DFCAF3A99AF636C1303DAE514F4EBE7CD11E2`, 8,944,269 bytes; extracted root confirmed. Reference commit NOT verifiable locally (no git metadata).
- Architecture preservation (code-level): the modified tree keeps the original modules (planner/worker, asset+exploration graphs, traffic proxy, evidence store, guard/intercept, notify, selfupdate, db). All changes are **additive** in `agent/` plus small edits to `tools.go`/`worker.go`/`go.mod` — no original module removed or rewritten. `go test ./agent/` passes (`ok ... 0.278s`).
- Security comparison: published baseline has **no** request-scope enforcement (RoE removed upstream); modified v5 adds `scopeGuard` at the `doHTTP` chokepoint + dial-level accounting (proven: unauthorized → 0 dials).

## NOT verified (criteria remain open)
- Full file-level ADDED/REMOVED/CHANGED diff (hashing in progress; not reported as done).
- Controlled comparative detection run (both versions on the same bench) — not executed.
- Endpoint/auth coverage comparison; AttackPath comparison; performance/resource comparison; integration audit; `go test ./...`; `go test -race` (env: no gcc).

## Remaining deliverables
`PHASE8_3_ARCHITECTURE_PRESERVATION.md`, `PHASE8_3_BENCHMARK_DESIGN.md`, `PHASE8_3_DETECTION_COMPARISON.md`, `PHASE8_3_AUTH_COVERAGE.md`, `PHASE8_3_ATTACKPATH_COMPARISON.md`, `PHASE8_3_SECURITY_COMPARISON.md`, `PHASE8_3_PERFORMANCE.md`, `PHASE8_3_INTEGRATION_AUDIT.md`, `PHASE8_3_REGRESSION.md`.

## Continuation
1. Finish the hash diff; record ADDED/REMOVED/CHANGED.
2. Build baseline binary from the extracted archive (separate worktree); run both versions on `bench-vuln.py` with the same ground truth; record TP/TN/FP/FN + latency.
3. Then phases 5–10.
