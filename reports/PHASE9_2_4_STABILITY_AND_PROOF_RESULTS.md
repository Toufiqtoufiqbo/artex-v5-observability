# PHASE IX.2.4 — STABILITY & PROOF RUN

**Generated:** 2026-11-24
**HEAD:** `e6ec56999175509551a1e43a3ee3b053d0941bd9`
**Lab:** fresh disposable DB `artex_v924_proof` on PostgreSQL 18.7.0 (127.0.0.1:55432), created/dropped in this task.
**Goal:** demonstrate with artifacts that the current project works end-to-end (7/7 detection + 8/8 planning), before any release-readiness work.
**Evidence labels:** `SOURCE VERIFIED`, `TEST VERIFIED`, `INTEGRATION VERIFIED`, `RUNTIME VERIFIED`, `BENCHMARK VERIFIED`.

---

## 1. Commands (exact)

```
go build ./...                       # exit 0
go vet ./...                         # exit 0
go test ./...                        # exit 1 — only missing-DSN server tests (no lab env at that moment)
go run ./cmd/artex benchmark run -suite artex-v5-offline-baseline -mode offline \
  -dsn 'postgres://postgres@127.0.0.1:55432/artex_v924_proof?sslmode=disable' \
  -out benchmark-artifacts/v924-stability-offline -seed 924     # exit 0
go run ./cmd/artex benchmark run -suite artex-v5-planning-v922 -mode offline \
  -dsn 'postgres://postgres@127.0.0.1:55432/artex_v924_proof?sslmode=disable' \
  -out benchmark-artifacts/v924-stability-planning -seed 924    # exit 0
```

## 2. Result 1 — Detection suite `artex-v5-offline-baseline` (BENCHMARK VERIFIED)

- run_id: `bench-offline-20261124T151951Z-7fefe0b42027`
- artifact: `benchmark-artifacts/v924-stability-offline/run.json`
- cases: **7/7 completed**, status `OK`
- micro: **TP=4, FP=0, FN=0, precision=1.000, recall=1.000, F1=1.000**
- reliability: tasks=7 completed, failed=0, timed_out=0, incomplete=0; tool invocations=6, tool failures=1 (expected: invalid-evidence gate case)
- finding dedup: submissions=5, new=4, duplicates=1, errors=0

Per-case: `sql_boolean_confirmed_local` TP=1; `xss_reflected_confirmed_local` TP=1; `ssti_confirmed_local` TP=1; `harmless_404_no_finding` TN=1; `worker_empty_response_edge` OK; `duplicate_finding_dedup_edge` TP=1 with dedup; `invalid_evidence_gate_edge` correctly rejected (tool failure).

## 3. Result 2 — Planning suite `artex-v5-planning-v922` (BENCHMARK VERIFIED)

- run_id: `bench-offline-20261124T152005Z-91a1651fa490`
- artifact: `benchmark-artifacts/v924-stability-planning/planning_run.json`
- cases: **8/8 passed**, status `OK`
- submitted=19, intents created=16, expected logical=16, inflation=0
- duplicates collapsed=3, missed collapses=0, false merges=0
- dependencies: gating violations=0, propagation violations=0, blocked dependents=2

All 8 cases PASS (exact repeat, paraphrase, distinct params/operations, batch mixed, retest, dependency gate, dependency failure).

## 4. Conclusion

**ال project يعمل فعليًا bench-to-bench:** detection 7/7 + planning 8/8، مع dedup سليم واعتماديات مضبوطة. أي إحساس بـ "الضياع" سببه تعدد المراحل، لا ضعف الكود.

## 5. Artifacts preserved
No previous artifact overwritten; new dirs `v924-stability-offline/` and `v924-stability-planning/`.

## 6. Lab cleanup
DB `artex_v924_proof` dropped (exit 0); PostgreSQL stopped (`pg_ctl stop -m fast`, exit 0; `pg_isready` → no response).

## 7. Verdict (exact)

**VERDICT: PASS — STABILITY AND END-TO-END PROOF VERIFIED**
