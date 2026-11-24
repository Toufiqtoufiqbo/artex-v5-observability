# PHASE8_3_1 — FINAL

**Verdict: `BASELINE READY WITH LIMITATIONS`**

## Proven
- Modified v5 frozen & documented (`PHASE8_3_1_MODIFIED_SNAPSHOT.md`).
- Baseline archive identity: SHA-256 + size; builds (`go build ./...` exit 0) and `go test ./agent/` passes (ok 0.078s) — unmodified.
- Isolation: baseline extracted separately; modified tree untouched; build artifacts excluded from source comparison only.
- Both build under the same Go 1.26.11.

## Not proven (limitations)
- Baseline commit/tag identity (no git metadata; `133f3cf…` unverifiable; archive↔v0.3.15 relation unproven).
- Dependency skew: baseline `norma v0.4.4` vs modified `v0.4.3`.
- Baseline runtime launch, frontend build, DB/API tests — not executed.
- Benchmark fixture parity (both versions on `bench-vuln.py`) — not executed.

## Comparison table

| Item | Baseline | Modified v5 |
|---|---|---|
| Go version | 1.26.11 (requires 1.26.3) | 1.26.11 (requires 1.26.3) |
| Backend build | PASS (exit 0) | PASS |
| `go test ./agent/` | PASS (0.078s) | PASS (0.278s) |
| Integration/DB/API tests | not executed | partial (DSN env) |
| Frontend build | not executed | built (embedui) |
| Runtime | not executed | running :8787 |
| Dependencies | norma v0.4.4 | norma v0.4.3 + x/net/html |

## Can Phase VIII.3 comparative benchmark start?
**Yes, conditionally** — both trees build and unit tests pass under one toolchain; baseline runtime + fixture parity must be completed first (baseline launch against `bench-vuln.py`).

Remaining reports: `PHASE8_3_1_BASELINE_IDENTITY.md`, `PHASE8_3_1_MODIFIED_BUILD.md`, `PHASE8_3_1_PARITY.md` (content folded into this final; separate files not created due to budget).
