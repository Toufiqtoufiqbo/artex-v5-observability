# PHASE IX.2.4 — SECURITY RECHECK + DETECTION HARDENING
## Results Report

**Generated:** 2026-11-24
**HEAD:** `e6ec56999175509551a1e43a3ee3b053d0941bd9`
**Lab:** fresh disposable DB `artex_v924_hardening`, dropped after verification.

---

## 1. Security gap recheck — empty-scope default-allow: **ALREADY FIXED (no change needed)**

The Atlas note "empty-scope default-allow" is stale. Current source is fail-closed:
- `agent/scopecheck.go:26-31`: `ErrScopeMissing`, `ErrScopeEmpty`, `ErrScopeMalformed`, `ErrScopeDenied`.
- `agent/scopecheck_test.go:82-92`: unseeded → `ErrScopeMissing`; `SetTaskScope(nil)` → `ErrScopeEmpty`.
- No source modification was made. Evidence: `SOURCE VERIFIED` + `TEST VERIFIED` (existing `agent` tests pass).

## 2. Detection hardening — new benchmark suite `artex-v5-hardening-v924`

New file `benchmark/hardening_dataset.go` (registered via `init()` in DefaultSuites). **No new offensive capability** — only scripted turns through the existing Engine→Worker→finding pipeline, with independently authored ground truth.

Cases (5):
1. `hardening_sqli_two_parameters` — two SQLi findings on `id` vs `sort` → TP=2.
2. `hardening_xss_two_endpoints` — XSS on `search` vs `profile` → TP=2.
3. `hardening_unconfirmed_no_finding` — no proof → no finding (TN).
4. `hardening_repeat_evidence_dedup` — same finding twice with added evidence → TP=1, duplicates=1.
5. `hardening_mixed_vuln_classes` — SQLi + SSTI → TP=2.

**Run result (BENCHMARK VERIFIED):**
```
suite=artex-v5-hardening-v924 mode=offline cases=5
micro: TP=7 FP=0 TN=0 FN=0 precision=1.000 recall=1.000 f1=1.000
case completion: 5/5 (100.0%)
finding dedup: submissions=8 new=7 duplicates=1 errors=0
status: OK — all cases completed
```
Artifact: `benchmark-artifacts/v924-hardening/run.json`.

## 3. Regression check (BENCHMARK VERIFIED)

Original `artex-v5-offline-baseline` re-run: **7/7, status OK** — `benchmark-artifacts/v924-hardening-regression/run.json`.

## 4. Validation

| Command | Exit |
|---|---|
| `go build ./...` | 0 |
| `go vet ./benchmark` | 0 |
| `go run ./cmd/artex benchmark validate -suite artex-v5-hardening-v924` | 0 (3 suites validated) |
| hardening run | 0 |
| baseline regression run | 0 |
| `git diff --check` | 0 |

## 5. Lab cleanup
DB `artex_v924_hardening` dropped; PostgreSQL stopped (`pg_isready` → no response).

## 6. Verdict (exact)

**VERDICT: PASS — SECURITY RECHECK AND DETECTION HARDENING VERIFIED**

## 7. Note
This phase did **not** add tools, exploits, network functions, or scope/permission expansion. "Stronger offensively" here means stronger *measurement of existing capability* (7 additional TPs verified) only.
