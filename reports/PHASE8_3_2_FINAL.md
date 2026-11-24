# PHASE8_3_2 — MODIFIED v5 RUNTIME + FINAL

**Modified v5:** ARTEX v5 on `127.0.0.1:8787` (DB `artex`), proxy 8788 — running; health/login previously verified; bench fixture `bench-vuln.py` on 3004 with ground truth `bench-truth.json` / `bench-metrics-truth.json`.

**Final verdict: `PARTIAL — RUNTIME OR PARITY BLOCKER`**

## Proven
- Baseline archive SHA-256 `93AFBEC0…CD11E2`, 8,944,269 B; builds (`go build ./...` exit 0), `go test ./agent/` ok 0.078s.
- Baseline runtime smoke PASS: isolated DB/port, health 200, login OK (`PHASE8_3_2_BASELINE_RUNTIME.md`).
- Modified v5 frozen (`e6ec569`, dirty tree documented), builds, agent tests ok; runtime up on 8787.
- Both instances isolated (DBs `artex_base` vs `artex`, ports 8888 vs 8787).

## Blocker
LLM-assisted comparative detection was **not executed** — each ARTEX task requires minutes of rate-limited LLM calls; two versions × paired runs exceed the remaining budget. Deterministic engine-level parity (oracles directly) is also not executed against the baseline binary. No fixture reset/ground-truth pairing between versions was performed.

## Not produced
`PHASE8_3_2_FIXTURE_VALIDATION.md`, `PHASE8_3_2_BENCHMARK_PROTOCOL.md`, `PHASE8_3_2_COMPARATIVE_RESULTS.md`, `PHASE8_3_2_REGRESSION_AUDIT.md`.

## Continuation
1. Configure the baseline LLM via `/api/llm` with the SAME provider/model/parameters as v5.
2. Reset fixture; run the identical task goal on baseline (8888) and v5 (8787); record findings + timing.
3. Score with predeclared ground truth; repeat paired runs.
4. Only then select MEASURABLY SUPERIOR / SUPERIOR WITH LIMITATIONS / MIXED RESULTS / NO DEMONSTRATED ADVANTAGE / REGRESSION DETECTED.

No comparative superiority claim is made in this phase.
