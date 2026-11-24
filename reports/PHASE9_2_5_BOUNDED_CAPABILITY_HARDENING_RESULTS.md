# PHASE IX.2.5 — BOUNDED CAPABILITY HARDENING
## Results Report

**Generated:** 2026-11-24
**HEAD:** `e6ec56999175509551a1e43a3ee3b053d0941bd9`
**Principle:** strengthen what exists, never add new tools, exploits, network/shell functions, scope or permissions.

---

## 1. What changed
`benchmark/hardening_dataset.go` — three new ground-truth cases added to `artex-v5-hardening-v924` (now 8 cases):
- `hardening_traversal_confirmed` — path traversal with reproducible proof → TP=1.
- `hardening_error_sqli_distinct` — error-based SQLi distinct from boolean SQLi → TP=1.
- `hardening_idor_no_proof` — suspected IDOR without reproduced access → no finding (no FP).

No application/tool source was modified; only independent ground truth and scripted (offline) turns.

## 2. Run result (BENCHMARK VERIFIED)
```
suite=artex-v5-hardening-v924 mode=offline cases=8
micro: TP=9 FP=0 TN=0 FN=0 precision=1.000 recall=1.000 f1=1.000
case completion: 8/8 (100.0%)
finding dedup: submissions=10 new=9 duplicates=1 errors=0
status: OK — all cases completed
```
Artifact: `benchmark-artifacts/v925-bounded-hardening/run.json`.

## 3. Boundary proof
- No new capability: the new cases reuse existing Probe/Finding pipeline; no new tool, provider, scope or permission path.
- Existing detection of distinct parameters/endpoints/classes remains verified (TP=2 cases still pass).
- Negative cases continue to produce zero FP.

## 4. Validation
| Command | Exit |
|---|---|
| `go build ./...` | 0 |
| `benchmark validate -suite artex-v5-hardening-v924` | 0 |
| hardening run | 0 |
| `git diff --check` | 0 |

## 5. Verdict (exact)

**VERDICT: PASS — BOUNDED CAPABILITY HARDENING VERIFIED (8/8, TP=9, FP=0; NO NEW OFFENSIVE CAPABILITY)**
