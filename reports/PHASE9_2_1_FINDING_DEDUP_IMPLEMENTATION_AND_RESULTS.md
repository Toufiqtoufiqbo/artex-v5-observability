# PHASE IX.2.1 — FINDING DEDUPLICATION & IDEMPOTENCY
## IMPLEMENTATION AND RESULTS

**Final verdict: `PASS — FINDING DEDUPLICATION AND IDEMPOTENCY VERIFIED`**

**Generated:** 2026-11-23 · HEAD `e6ec56999175509551a1e43a3ee3b053d0941bd9` (detached) · Go 1.26.11 windows/amd64 · PostgreSQL 18.7.0

---

## 1. Executive summary

ARTEX v5 had **no production finding deduplication at all**. The only dedup was `benchmark/fingerprint(vulnClass, name, severity)`, which runs *after* persistence and affects scoring only. `findings` had four ordinary indexes and a primary key — no semantic uniqueness.

IX.2.1 closes that gap without building a parallel subsystem: one canonical, versioned identity (`v1:…`), a partial database unique index scoped per task, idempotent `ON CONFLICT` resolution in the single production persistence primitive (`db.RecordFindingTx`), and evidence attachment on duplicates instead of discarding them. Baseline defect was reproduced first on a disposable PostgreSQL database (2 sequential → 2 rows, 20 concurrent → 20 rows, 3 retries → 3 rows). After the fix: sequential/concurrent/retry all yield **one logical finding**, distinct findings stay distinct, evidence integrity tests pass, and the IX.1 baseline is unchanged (TP 4→4, FP 0→0, 7/7 completed).

`RACE DETECTOR UNKNOWN — ENVIRONMENT BLOCKED` remains unchanged. This phase did not run the race detector and does not claim race freedom.

---

## 2. Initial repository state (recorded before edits)

- Branch `HEAD` (detached), HEAD `e6ec56999175509551a1e43a3ee3b053d0941bd9`.
- Tree: **82 entries — 23 modified, 59 untracked** (full list in the forensic audit §0).
- Baseline `go build ./...` exit 0.
- Go and PostgreSQL binaries exist outside `PATH`; both were prefixed per command. Port 5433 free, no lab processes.
- Final tree: **95 entries — 30 modified, 65 untracked**: every original entry preserved; 7 new tracked-file modifications and 6 new untracked files (+ one new file inside the already-untracked `benchmark/`). No destructive Git operation, no commit, no push, no user DB touched.

---

## 3. Existing deduplication architecture (Gate A)

Full forensic record: `reports/PHASE9_2_1_FINDING_DEDUP_FORENSIC_AUDIT.md`.

Execution map:

```
Tool result → report_finding (agent/tools.go:1219)
  → gateFindingEvidence (finding_gate.go:61)  [rejects solution leaks / unconfirmed]
  → db.RecordFindingInput
  → evidence.Store.Record (evidence/store.go:322)  OR  ExplorationStore.RecordFinding
  → db.RecordFindingTx (db/finding_traffic.go:342)
  → PostgreSQL findings + traffic bindings
  → API / export
```

Classification before the fix: **`NO_DEDUP`**. No application check, no DB constraint, no fingerprint column, no `ON CONFLICT`. Only benchmark scoring deduped (by `vulnclass\x00name\x00severity`, after persistence).

---

## 4. Existing duplicate-check callers (complete inventory)

| Path | Role | Dedup before fix |
|---|---|---|
| `agent/tools.go:1219 report_finding` | Worker/Planner/MainAgent ToolSet | none |
| `evidence/store.go:322 Store.Record` | production recorder (`server/task_llm.go:565/575/590`, `benchmark/runner.go:50-51`) | none |
| `db/finding_traffic.go:411 RecordFinding` | legacy no-recorder path | none |
| `db/finding_traffic.go:342 RecordFindingTx` | single persistence primitive | none |
| `db/findings.go:82 AddFinding` / `db/exploration.go:2019 AddStandaloneFinding` | test-only direct insert | none |
| `evidence/store.go:337 Store.Bind` | evidence attachment | binding dedup by snapshot ID (distinct concept, retained) |
| `benchmark/matcher.go:91` | evaluation only | fingerprint (after persistence) |
| retest paths | re-report through tool above | none |

No HTTP API creates findings directly. Both production callers funnel through `RecordFindingTx`, so that primitive was the correct single enforcement point.

---

## 5. Baseline defect reproduction (disposable DB `artex_ix212`)

Migration role `artex_mig_ix212` applied `db/schema.sql` (49 tables, exit 0); runtime role `artex_runtime_ix212` (DML only) executed all writes.

| Scenario | Result (before) |
|---|---|
| S1 two sequential equivalent inserts | `S1_ROWS = 2` |
| S2 two concurrent producers × 10 inserts | `count = 20` (both processes exit 0) |
| S3 same finding, different producer metadata | `S3_ROWS = 2` |
| S4 retry after uncertain response ×3 | `S4_ROWS = 3` |
| S5 schema inventory | 1 unique constraint (`findings_pkey` on `id`) + 4 ordinary indexes; no semantic uniqueness |

 reproduced exactly as described; no production schema or user database was modified during reproduction.

---

## 6. Canonical Finding identity contract

Authoritative specification: `reports/FINDING_IDENTITY_SPECIFICATION.md`.

```
fingerprint = "v1:" + hex(sha256(
  "v1" NUL scope NUL norm(vulnclass) NUL norm(name) NUL norm(asset_ids)
))
```

- **Scope** = decimal `task_id` (>0), else `legacy`.
- **Included**: task scope, vulnclass, name, affected asset ID set.
- **Excluded**: severity (mutable via `SetFindingSeverity`), summary, evidence, worker, status, report, timestamps, node/intent IDs, traffic refs.
- Benchmark's old fingerprint was **not** reused: it omitted task/target and included mutable severity.
- Implementation: `db/finding_identity.go::FindingFingerprint`.
- Table-driven contract tests (`db/finding_identity_test.go`): identical findings, different types/targets/endpoints/parameters/methods, missing/empty fields, case normalization, asset order/duplicates, encoding differences, evidence-independent identity, determinism/format — **PASS**.

---

## 7. Canonicalization rules

- Text: Unicode lowercase, outer trim, internal whitespace runs → single ASCII space. Punctuation, numbers, escape/percent sequences preserved (meaningful).
- Assets: positive IDs only, dedupe, numeric sort, decimal comma-join; empty → `""`.
- Hostname case/default ports/IPv6/URL path/query/encoding: **delegated to the asset subsystem** (`uq_av2_*`, `inet`) — the fingerprint uses stable asset IDs, so no risky URL reimplementation.
- HTTP method / parameter / endpoint must be encoded in `name` by producers (no dedicated columns exist). Documented contract requirement; covered by tests.

---

## 8. Scope and uniqueness policy

- Unique **per task** (`task_id > 0`).
- `task_id IS NULL` (findings retained after task deletion) and NULL fingerprints are **excluded** from the index — historical records are never merged, deleted, or rewritten.
- No global/tenant uniqueness invented; cross-task equivalence remains two findings (explicitly tested).

---

## 9. Application-level dedup behavior (Gate C)

`db.RecordFindingTx` now:
1. Holds `LockTaskEvidenceTx` (existing task row lock).
2. Computes the canonical fingerprint.
3. Fast-path `SELECT` for an existing identity → `Outcome="duplicate"`.
4. On duplicate: records a provenance `yields` edge from this intent to the existing finding node (only if absent), attaches new traffic evidence via the existing idempotent `AddFindingTrafficTx`, and returns the existing IDs. **No new finding, no discarded evidence, no new notification.**
5. On new: inserts the exploration node, then `INSERT … ON CONFLICT … DO NOTHING RETURNING id`; a lost race removes the speculative node and reuses the winner.
6. Anchors/edges/notification event are created only on first insert.

All three agent runtimes use the same ToolSet + recorder → same contract. Sequential/concurrent/retry tests in `db/finding_dedup_test.go` pass.

---

## 10. Database-level uniqueness design (Gate D)

`db/schema.sql` (reversible, non-destructive):

```sql
ALTER TABLE findings ADD COLUMN IF NOT EXISTS finding_fingerprint TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS uq_findings_identity_v1
  ON findings(task_id, finding_fingerprint)
  WHERE task_id IS NOT NULL AND finding_fingerprint IS NOT NULL;
```

`ON CONFLICT (task_id, finding_fingerprint) WHERE task_id IS NOT NULL AND finding_fingerprint IS NOT NULL DO NOTHING` is used in both insert paths. All writes were executed by the restricted runtime role with DML-only privileges; no DDL was required at runtime.

---

## 11. Migration design and safety

- Column is **nullable**, index is **partial** → historical rows untouched; no backfill, no full-table rewrite, no automatic merge/delete.
- Reversible: `DROP INDEX IF EXISTS uq_findings_identity_v1; ALTER TABLE findings DROP COLUMN IF EXISTS finding_fingerprint;`.
- Migration role applied schema; runtime role used only for DML. No superuser used for routine operations.
- Duplicate inventory was produced during forensic baseline (§5); historical findings preserved.

---

## 12. Idempotency semantics

- Semantic dedup and request idempotency share the same persisted, task-scoped fingerprint — no separate distributed idempotency store.
- Retry after uncertain response re-derives the same identity, returns the committed finding; evidence binding dedups by `(finding_id, snapshot_id)`; notification event is created once.
- Test: `TestFindingDedupRetryIdempotent_v921` — 3 attempts → 1 finding, 1 notification event.

---

## 13. PostgreSQL concurrency results

All tests used real PostgreSQL and barrier channels (no sleeps):

| Test | Result |
|---|---|
| `TestFindingDedupSequential_v921` | first=`new`, second=`duplicate`, same IDs, 1 row |
| `TestFindingDedupConcurrent_v921` (12 goroutines) | exactly 1 `new`, 11 `duplicate`, 1 logical ID, 1 row |
| `TestFindingDedupDirectInsertConflict_v921` (8 goroutines, no task lock) | all return same ID, 1 row |
| `TestFindingDedupDatabaseEnforcesUniqueness_v921` | second raw INSERT → SQLSTATE `23505` |
| `TestFindingDedupUnrelatedErrorsStayVisible_v921` | foreign intent → error, no finding returned |
| `TestFindingDedupDistinctFindingsRemainDistinct_v921` | 3 distinct + 1 cross-task = 4 findings |

Result: `go test ./db -run 'TestFindingDedup|TestFindingFingerprint|TestNormalizeIdentityAssets' -count=1` → **ok** (exit 0).

---

## 14. Evidence-binding preservation (Gate E)

`evidence/dedup_integrity_test.go` verifies: duplicate with new evidence → same finding, 3 bindings; original notes ("original baseline"/"original proof") not overwritten; identical repeat → bindings unchanged; invalid traffic ID still errors even though the finding exists; `ValidateFindingEvidence` remains `EvidenceValid` with 3 bindings. Existing `TestEvidenceBindingLifecycle` was adjusted to use a distinct title for its cross-finding snapshot-sharing assertion; package `evidence` passes fully on the clean DB. `LockFindingEvidenceTx`, `ValidateFindingEvidence`, cross-task rejection, retest linkage and export validity were not modified.

---

## 15. Finding / Gate integration

`gateFindingEvidence` unchanged (solution-leak and unconfirmed-evidence rejection intact). Duplicate resolution occurs inside `WithEvidenceTx`, after task/intent validation and after the gate, so a duplicate cannot bypass authorization checks. `benchmark` case `invalid_evidence_gate_edge` still passes (TP=0, FP=0, 1 tool failure).

---

## 16. Planner / Worker integration (Gate H)

- `Planner.Plan` (`agent/planner.go:355`) and `Worker.Execute`/`ExecuteWithMessage` (`agent/worker.go:350-361`) each build a ToolSet with the same recorder; all finding submissions converge on `RecordFindingTx`.
- Engine `runIntent` already dedups chat re-injection across `model_error` retries via `requestID` transcript marker (`server/engine.go:974-1016`); `steerHooks` wraps `t.Guard.Hooks()` unchanged.
- Planner can still emit repeated equivalent *intentions* (prompt-level only) — this is **not fixed here** and is the subject of IX.2.2. Finding-level dedup prevents duplicate persistence regardless.
- No Planner/Worker reasoning or tool behavior was changed; only an additive `dedup_outcome` field on the recorded result.

---

## 17. Observability additions (Gate G)

`db/finding_dedup_telemetry.go`: atomic aggregate counters (submissions, new, duplicates, evidence attachments, conflicts, errors) exposed via `FindingDedupStats()`. Structured log lines have fixed format and bounded fields (`outcome`, `ok`) — no finding IDs, titles, evidence payloads, or high-cardinality labels. Telemetry performs only atomic increments; it cannot alter persistence or authorization decisions. Benchmark per-case deltas are exposed as `CaseMetrics.Dedup` and printed in the CLI summary.

---

## 18. Benchmark before/after comparison (Gate F)

Commands (new artifact dirs; originals untouched):

```
artex benchmark run -suite artex-v5-offline-baseline -mode offline -out benchmark-artifacts/v921-regression-offline
artex benchmark run -suite artex-v5-dedup-v921 -mode offline -out benchmark-artifacts/v921-dedup-offline
artex benchmark compare -runs benchmark-artifacts/v91-baseline3/run.json,benchmark-artifacts/v921-regression-offline/run.json -out benchmark-artifacts/v921-compare-baseline.json
```

| Run | Cases | TP | FP | F1 | Dedup (submissions/new/duplicates) |
|---|---|---|---|---|---|
| `v91-baseline3` (IX.1, preserved) | 7/7 | 4 | 0 | 1.000 | not instrumented |
| `v921-regression-offline` | 7/7 | 4 | 0 | 1.000 | 5 / 4 / 1 |
| `v921-dedup-offline` (6 new cases) | 6/6 | 8 | 0 | 1.000 | 12 / 8 / 4 |

Comparison: `incompatible: ok`, **TP 4→4 (Δ+0/−0), FP 0→0 (Δ+0/−0)**. Dedup suite: 12 submissions → 8 logical findings, 4 duplicates; exact repeat, retry×3, additional evidence, distinct parameters and distinct vuln classes all matched independent ground truth. A reduced raw count was **not** claimed as an accuracy improvement. Original artifacts `v91-baseline{,2,3}` were not overwritten.

---

## 19. Test results

| Command | Result |
|---|---|
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `git diff --check` | exit 0 |
| `go test ./db -run 'TestFindingDedup…'` (clean DB) | ok |
| `go test ./evidence -run 'TestEvidenceBindingLifecycle\|TestFindingDedup…'` | ok |
| `go test ./server -run 'TestFindingGroups…\|TestFindingWorkflow…'` | ok |
| `go test ./... -count=1` (clean DB) | 11 packages ok; `agent`, `db`, `llmrec`, `server` have **pre-existing environmental failures** (below) |

Pre-existing/environmental failures (not introduced by IX.2.1; none touch finding identity):
- Restricted runtime role cannot DDL: `llm_records`/`llm_usage` relation missing or `permission denied for schema public` — tests `TestRecorderPersistsRawWireBodies`, `TestLLMRecordsMigrate…`, `TestSideTaskArchiveVersions`, `TestFindingTrafficArchiveV3RoundTripAndRetry`, `TestSideUsageRecordedOnceOnConsumerCancellation`, etc.
- Windows: `TestSkillRelPath` (`\` vs `/`), symlink prerequisites, temp-dir cleanup races (`TestInheritedActivityDetailAndRelationDeletion`, `TestSyntheticSecretsNotExposedViaProfilesAPI`, `TestWorkerControlRoutes`).
- Missing local tooling/seeding: `python3 unavailable`, `TestOpenSeed`/`TestConfigStores`/`TestMgmtAPI` (builtin agents), `ARTEX_REVIEW_LIVE_CONFIG` (live test), `TestCoreTaskLifecyclePG` (no LLM provider), `TestGraphOverviewExpandsAssociatedCompanyScope` (known flaky from IX.1.1).
- `TestCompanyScopeAttribution` fails standalone with an unrelated company_id attribution mismatch in the disposable DB.

Two legacy fixtures were updated **only** because they intentionally created two same-identity findings (`db/findings_test.go`, `server/findings_groups_test.go`) or expected a duplicate report to create a new unbound finding (`server/finding_workflow_test.go`); `evidence/store_test.go` now uses a distinct title for its cross-finding snapshot-sharing assertion. Each retains its original testing purpose.

`RACE DETECTOR UNKNOWN — ENVIRONMENT BLOCKED` (unchanged; ordinary concurrency tests are not race-detector runs).

---

## 20. Files changed and justification

**Modified production/schema:**
- `db/schema.sql` — nullable fingerprint column + partial unique index (the migration).
- `db/finding_traffic.go` — identity computation, fast-path reuse, ON CONFLICT race handling, provenance edge, additive `Outcome`.
- `db/findings.go` — `AddFinding` uses the same identity + conflict resolution.
- `benchmark/{metrics,runner,cli}.go` — expose dedup counters per case and in summary.

**New:**
- `db/finding_identity.go` — canonical v1 identity function.
- `db/finding_dedup_telemetry.go` — bounded atomic counters + fixed-format logs.
- `benchmark/dedup_dataset.go` — 6-case IX.2.1 dedup suite.
- `db/finding_identity_test.go`, `db/finding_dedup_test.go`, `db/finding_dedup_db_test.go`, `evidence/dedup_integrity_test.go` — regression tests.

**Test-fixture adjustments:** `db/findings_test.go`, `server/{findings_groups_test,finding_workflow_test}.go`, `evidence/store_test.go` (§19).

**Reports/artifacts (outside repo tree):** this report, `FINDING_IDENTITY_SPECIFICATION.md`, `PHASE9_2_1_FINDING_DEDUP_FORENSIC_AUDIT.md`; new benchmark artifacts `v921-regression-offline/`, `v921-dedup-offline/`, `v921-compare-baseline.json`. `v91-baseline{,2,3}/` untouched.

---

## 21. Remaining limitations

- v1 identity uses `name` as the resource/parameter/method discriminator; producers must make titles specific. Two different issues with identical `vulnclass`, `name` and asset set are one logical finding by design.
- `task_id IS NULL` legacy findings are intentionally not deduplicated or backfilled; historical duplicates remain (no destructive migration).
- No cross-task/tenant semantic dedup (schema has no tenant dimension).
- Benchmark offline mode is scripted; live-model mode remains unexercised (IX.2 roadmap).
- Full `go test ./...` is not green in this environment due to pre-existing permission/tooling/Windows issues (§19).
- `RACE DETECTOR UNKNOWN` remains open.

---

## 22. Recommendations for IX.2.2 — Planner Intent Management

1. Add code-level intent dedup in `agent/planner.go` (structural key on intent summary/type/target), mirroring the finding identity approach — prompt-level restraint alone is insufficient.
2. Add direct unit tests for `Planner.Plan` and `Worker.ExecuteWithMessage` (currently no direct coverage) before changing their logic.
3. Decide whether repeated intents should be merged in `add_intent` (`agent/tools.go:1032`) with an outcome field analogous to `dedup_outcome`.
4. Reuse `FindingDedupStats()`-style bounded counters for intent dedup; do not introduce an observability service.
5. Keep the finding-level contract unchanged; IX.2.2 should not reopen identity semantics.

---

## 23. Final verdict

**`PASS — FINDING DEDUPLICATION AND IDEMPOTENCY VERIFIED`**

All twelve PASS conditions are met: existing impl audited (Gate A); identity documented and versioned (Gate B, spec); sequential and concurrent duplicates handled; database uniqueness enforced; retries idempotent; distinct findings preserved; evidence/provenance intact; restricted runtime privileges retained; benchmark shows correct duplicate handling with no baseline regression; build/vet/relevant regression tests pass; previous artifacts and the complete pre-existing working tree preserved.

Caveats are explicit: environmental test failures listed in §19 are pre-existing and unrelated; `RACE DETECTOR UNKNOWN` is unchanged; a lower finding count is not claimed as improved accuracy.

**One logical finding, consistent evidence, safe retries, and no accidental duplication.**
