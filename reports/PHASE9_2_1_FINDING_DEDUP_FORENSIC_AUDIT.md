# PHASE IX.2.1 â€” GATE A: FINDING DEDUPLICATION FORENSIC AUDIT

**Mode:** read-only on application source. No application code, schema, or config was modified during this audit.
**Generated:** 2026-11-23 Â· HEAD `e6ec56999175509551a1e43a3ee3b053d0941bd9` Â· branch `HEAD` (detached)

---

## 0. Initial repository state (recorded before any work)

| Item | Value |
|---|---|
| Branch | `HEAD` (detached) |
| HEAD | `e6ec56999175509551a1e43a3ee3b053d0941bd9` |
| Working tree | **82 entries** â€” 23 modified, 59 untracked |
| Modified | `.gitignore`, `agent/{chat,mainagent,planner,provider,provider_capture_test,tools,worker}.go`, `cmd/artex/main.go`, `db/db.go`, `enrich/enrich.go`, `evidence/store.go`, `selfupdate/{github,stage}.go`, `server/{assembly,auth,customtool,dto,finding_retests,finding_traffic,manager,server}.go`, `traffic/traffic.go` |
| Untracked | 59, incl. `agent/{finding_gate,finding_recorder,httpclient,scopecheck,tools_probe,tools_retest,tools_attackpath,tools_diff,tools_http,tools_login,toolcatalog}.go`, `benchmark/`, `evidence/v8_6_*`, `server/v8_6_3_*`, `traffic/egress*`, plus test files |
| Baseline build | `go build ./...` â†’ exit **0** (Go 1.26.11 windows/amd64 from `<GO_BIN>`) |
| PostgreSQL | 18.7.0 from `<WORKSPACE>\pg\...\bin` â€” **not in PATH**, port 5433 free |
| Race detector | `RACE DETECTOR UNKNOWN â€” ENVIRONMENT BLOCKED` (preserved; not claimed resolved) |

Toolchain note: Go and PostgreSQL binaries exist outside `PATH`; every command in this phase prefixes `PATH` with both install directories. No system/user database was touched.

---

## 1. Execution map â€” Probe/Tool result â†’ persistence â†’ API/export

```
Probe / HTTP / Fuzz / IDOR tool result (agent/tools_probe.go, tools_diff.go, tools_http.go)
        â”‚  (verdict is a Go measurement, not model intuition)
        â–¼
report_finding tool  (agent/tools.go:1219 addFinding)
        â”‚  gateFindingEvidence(evidence, proof, proofType)   [agent/finding_gate.go:61]
        â”‚  â†’ rejects solution-leak / "not confirmed" evidence
        â–¼
db.RecordFindingInput  {TaskID, ExplorationID, IntentID, VulnClass, Name,
                        Severity, Summary, Evidence, Worker, AssetIDs}
        â”‚
        â”œâ”€â”€ t.findingRecorder != nil  â†’ evidence.Store.Record  (evidence/store.go:322)
        â”‚        WithEvidenceTx â†’ LockTaskEvidenceTx â†’ prepare(refs) â†’ db.RecordFindingTx
        â”‚
        â””â”€â”€ else (legacy/no-recorder) â†’ ExplorationStore.RecordFinding (db/finding_traffic.go:411)
                 WithEvidenceTx â†’ db.RecordFindingTx(ctx, tx, in, nil)
        â–¼
db.RecordFindingTx  (db/finding_traffic.go:342)
        â”‚  LockTaskEvidenceTx â†’ validate taskâ†”exploration â†’ validate intent
        â”‚  INSERT exploration_nodes(kind='finding') RETURNING id
        â”‚  INSERT exploration_anchors â€¦ ON CONFLICT DO NOTHING
        â”‚  INSERT exploration_edges(intentâ†’finding)
        â”‚  INSERT findings(...) RETURNING id
        â”‚  RecordNotificationEventTx (same transaction, savepoint-isolated)
        â”‚  AddFindingTrafficTx â†’ FindingTrafficTx
        â–¼
PostgreSQL findings + finding_traffic_bindings + traffic_evidence_snapshots
        â–¼
API / export: db.ListFindings*, ListFindingGroups, GetFinding,
              report/findings.go (FindingsMarkdown/CSV), server finding_traffic export
```

**No deduplication step exists anywhere in this chain.**

---

## 2. Caller inventory (complete)

| # | Path | Entry point | Dedup? | Scope / notes |
|---|---|---|---|---|
| 1 | `agent/tools.go:1219` `report_finding` | Worker / Planner / MainAgent ToolSet (all three inject the same recorder via `SetFindingRecorder`) | **No** | Only evidence gate + task/intent validation |
| 2 | `evidence/store.go:322` `Store.Record` | `server/task_llm.go:565/575/590` wires it as the recorder for Worker/Planner/MainAgent; `benchmark/runner.go:50-51` | **No** | Always calls `RecordFindingTx` |
| 3 | `db/finding_traffic.go:411` `ExplorationStore.RecordFinding` | legacy path used when no recorder and no traffic refs; `agent/finding_recorder_test.go`, `db/finding_traffic_archive_test.go` | **No** | Same `RecordFindingTx` |
| 4 | `db/finding_traffic.go:342` `RecordFindingTx` | called by #2 and #3 | **No** | The single persistence primitive |
| 5 | `db/findings.go:82` `DB.AddFinding` | tests only (`db/findings_test.go`, `server/*_test.go`), plus wrapper `db/exploration.go:2019 AddStandaloneFinding` (tests only) | **No** | Direct INSERT, no exploration node, no evidence |
| 6 | `benchmark/matcher.go:91-115` `MatchFindings` | evaluation only (`benchmark/`) | **Yes â€” fingerprint** | `fingerprint(vulnClass, name, severity)` = normalized `vulnClass\x00name\x00severity`; dedups actual and expected lists **after** persistence, purely for scoring |
| 7 | `evidence/store.go:337` `Store.Bind` | manual/agent evidence attachment | N/A (no new finding) | Binding dedups by snapshot ID (`finding_traffic_bindings` UNIQUE(finding_id, snapshot_id)); distinct concept |
| 8 | Retest paths (`server/finding_retests.go`, `agent/tools_retest.go`) | produce retest verdicts, may re-report via tool #1 | **No** | The re-report goes through the same undeduplicated path |

**Finding creation entry points used in production: one effective primitive (`RecordFindingTx`) with two callers (#2 recorder path, #3 legacy path). No HTTP API creates findings directly.** Updating one insertion method *would* therefore cover the production paths â€” but there is currently no check to update.

---

## 3. Existing behavior classification

**`NO_DEDUP`** for production persistence.

Evidence:
- `db/schema.sql:1081-1109` â€” `findings` has only `findings_pkey` (UNIQUE on `id`) plus four ordinary indexes:
  `idx_findings_task(task_id, created_at DESC)`, `idx_findings_time`, `idx_findings_status`, `idx_findings_asset_ids` (GIN).
- `pg_constraint` query on the disposable DB: exactly **1** unique/PK constraint, on `id` only. A non-unique index is not a uniqueness guarantee.
- No `ON CONFLICT` on any `findings` INSERT; `RecordFindingTx:366-388` and `db.AddFinding:95-99` are plain `INSERT â€¦ RETURNING`.
- No fingerprint/hash/identity column exists on `findings`.
- The only dedup function in the repository is `benchmark/fingerprint`, which is **evaluation-only** and uses only 3 fields (no task, target, endpoint, parameter, or evidence).

Classification is **not** `APPLICATION_ONLY` (no production application check), **not** `DATABASE_ENFORCED`, **not** `PARTIALLY_ENFORCED`, **not** `UNKNOWN`.

---

## 4. A3 â€” baseline defect reproduction (fresh disposable PostgreSQL)

**Database:** `artex_ix212` on 127.0.0.1:55432, owner `artex_mig_ix212` (migration role); application statements executed as `artex_runtime_ix212` (restricted runtime role). Schema applied from the repository's `db/schema.sql` â†’ **49 tables**, exit 0. No existing user DB was used.

| Scenario | Procedure | Exact rows observed |
|---|---|---|
| S1 â€” two sequential equivalent inserts | Two identical `INSERT`s (SQLi / "Blind SQL Injection (boolean)" / high) | `S1_ROWS = 2` |
| S2 â€” two concurrent producers | Two parallel `psql` processes, each `INSERT â€¦ SELECT â€¦ generate_series(1,10)` with identical identity | `count = 20` (10 + 10), both processes exit 0 |
| S3 â€” same finding via two producer paths | Identical XSS finding with `worker='worker-A'` and `worker='worker-B'`, same asset_ids | `S3_ROWS = 2` |
| S4 â€” retry after uncertain response | Same SSTI finding inserted 3 times | `S4_ROWS = 3` |
| S5 â€” duplicate-check race | No application duplicate check exists to race (code audit Â§2); DB has no semantic constraint | n/a â€” structurally impossible to protect |

Final count after S1+S3+S4: `S_TOTAL = 7`. After S2: `_CONC = 20`.

**Interpretation:** every equivalent insertion created a new logical finding. Neither sequential, concurrent, cross-producer, nor retry semantics are idempotent today. The benchmark's dedup cannot help: it runs *after* the rows exist and only affects scoring.

---

## 5. Specific questions from the mission brief â€” answered

1. **Where does duplicate detection occur?** Only in `benchmark/matcher.go` (evaluation).
2. **Before or during persistence?** Neither â€” after persistence, at scoring time.
3. **Which fields participate?** Benchmark only: vulnclass, name, severity (normalized with `normalizeText`).
4. **Task-scoped or global?** Neither is enforced; benchmark fingerprint is global across the case's actual list.
5. **Is evidence compared?** No.
6. **Are concurrent insertions protected?** No (S2 = 20 rows).
7. **Different producers, different normalization?** All producers hit the same primitive, but no normalization is applied at all; worker name is stored but never used for identity.
8. **Do updates bypass dedup?** There is no dedup to bypass; `SetFindingStatus/Severity/Name/VulnClass` update in place.
9. **Do imported/derived findings use the same rules?** No import path exists; retest re-reports through the same undeduplicated tool path.
10. **Can APIs create findings through another path?** No HTTP route performs a findings INSERT. `AddFinding`/`AddStandaloneFinding` are test-only.

---

## 6. Forensic conclusions (carry into Gate B)

- The existing "deduplication" is a **scoring convenience in the benchmark**, not a product guarantee. Phase IX.2.1 must *complete* the missing guarantee, not replace a working subsystem.
- `RecordFindingTx` is the correct single enforcement point: both production callers funnel through it, and it already owns an exclusive task-level evidence lock (`LockTaskEvidenceTx`) â€” a natural place to serialize duplicate resolution.
- The identity contract cannot be `benchmark.fingerprint`: (a) it omits task/target/endpoint/parameter, so it would merge distinct findings across tasks; (b) severity is mutable via `SetFindingSeverity`, so a fingerprint containing severity is unstable; (c) it is not versioned.
- `findings.task_id` is nullable (`ON DELETE SET NULL`), so a scope policy must define behavior for `NULL` task rows (historical findings preserved across task deletion).
- Evidence and provenance are already protected by `finding_traffic_bindings` UNIQUE(finding_id, snapshot_id), `LockFindingEvidenceTx`, and read-time validation. Deduplication must **reuse** these rather than replace them.
- Restricted runtime role successfully performed all baseline INSERTs with DML-only privileges â€” no regression observed; must be re-verified after the schema change.

---

## 7. Deliverable / stop-condition check

- `reports/PHASE9_2_1_FINDING_DEDUP_FORENSIC_AUDIT.md` â€” this file.
- Application source: **unchanged**.
- Existing user databases: **untouched**.
- Disposable DB `artex_ix212` created for this phase only; will be dropped after all GATE D/F verification, after confirming exact name and that no process uses it.
- No destructive Git operations, no commits, no pushes, no sleeps used as concurrency barriers (S2 used parallel processes without sleeps).

**Gate A verdict: `NO_DEDUP` confirmed with reproducible evidence. Proceed to Gate B (identity contract) before any code changes.**
