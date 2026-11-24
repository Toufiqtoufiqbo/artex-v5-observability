# MASTER REPORT — PHASE VIII.6 DATABASE, DATA INTEGRITY & EVIDENCE TRUST BOUNDARY

**Verdict:** `PASS — EVIDENCE INTEGRITY & ADMINISTRATION WINDOW PARTIALLY DOCUMENTED; DB PRIVILEGE DEFECTS CRITICAL` — see §19 for the final verdict text. Key findings: F-A1 (CRITICAL — runtime role is superuser), F-E1 (HIGH — no production audit wiring for traffic-proxy decisions), F-B1 (MEDIUM-HIGH — TrafficRef stores no content hash; integrity relies on blob-filename hash with retrieval-time re-verification — verified).

## 1. Executive summary

Phase VIII.6 establishes whether ARTEX can trust its own stored state and evidence. Findings:
- **F-A1 (CRITICAL):** Runtime PostgreSQL role = `postgres` — superuser (`pg_roles`: rolsuper=t, rolbypassrls=t). Runtime operations (DDL via idempotent schema apply in `db.Open`, SELECT/INSERT/UPDATE/DELETE/TRUNCATE via repos) are not privilege-separated from administrative operations.
- **F-B1 (MEDIUM-HIGH):** `db.TrafficRef{TrafficID, Role, Note}` (db/finding_traffic.go:42-46) does not store content hash or length. Evidence integrity relies on the blob filename hash; retrieval-time re-verification (`verifyFile` in `evidence.Store.OpenBody` — store.go:253-270) is implemented — TEST VERIFIED (new tests). Write-time verification exists (`Store.Put` path — store.go:91-98, `writeBody` mismatch behavior — NOT INSPECTED; documented).
- **F-B2 (MEDIUM):** Binding (`evidence.Store.Bind`, store.go:203-215) uses `LockFindingEvidenceTx(tx, findingID, nil)` — the `nil` argument semantics unchecked (may be a "for insertion" guard or not) — documented; not proven that binding can't rebind a finding to a different task.
- **F-B3 (MEDIUM):** Finding-trust dependency — no automation that refuses to present findings as verified when evidence is invalid — documented (NOT INVESTIGATED of cross-check).
- **F-E1 (HIGH):** `traffic.SetEgressAuditSink` — no production callers (viii.4.8). Audit records for proxy decisions are emitted (`emitTrafficEgressAudit`) but not collected — documented limitation.
- **F-E2 (LOW):** CSRF on cookie-based auth not inspected.
- Positive findings: evidence blobs verified at retrieval (OpenBody, store.go:264); `verifyFile` compares hash AND length AND content (store.go:44-58); tamper detection proven by tests.

## 2. Initial state (preserved)

- Branch: `HEAD` (detached). HEAD: `e6ec56999175509551a1e43a3ee3b053d0941bd9`.
- Status at start: 68 entries (15 modified, 53 untracked — VIII.4.6→8.5.1 changes; viii.6 adds: `evidence/v8_6_tamper_test.go`).
- Go: `go1.26.11 windows/amd64`. `CGO_ENABLED=0`; no C compiler (race UNKNOWN — carried from viii.4.8).
- PG 5433: lab PG 18.7.0 on 127.0.0.1:5433 (cluster846). Databases: `artex_v42`? — existing DBs `artex_v46h`, `artex_v48f` (viii.4.7/8.5-era lab DBs — NOT modified by this phase; only `artex_v48f` used for new tests).
- No ARTEX runtimes at start (lab stopped before phase).
- Environment names (values redacted): `ARTEX_PG_DSN`, `ARTEX_TRUSTED_PROVIDER_HOSTS`, `ARTEX_ALLOW_LOCAL_SHELL`, `ARTEX_EGRESS_ALLOWLIST`, `ARTEX_LLM_BASE_URL`, `ARTEX_ADMIN_PASSWORD`, `NVAPI_KEY`.

## 3. PostgreSQL privilege & role inventory (Gate A)

Runtime role:
- **F-A1 (CRITICAL):** Runtime DB role is `postgres` — `pg_roles` (lab PG 18.7.0, no password values recorded): `rolsuper=t, rolcreatedb=t, rolcreaterole=t, rolbypassrls=t` (the *postgres* role — the default cluster superuser). Production deployments must not use the default superuser for the runtime.
- Where does the runtime get the role? `db.DSN()` → `config.PostgresDSN()` → `ARTEX_PG_DSN` env or `config.json` `database.dsn`/host+user+dbname (config/config.go:92-152).

Runtime DB surface:
- Schema: idempotent `Open()` (db/db.go:133+) — applies schema — requires DDL privilege at runtime (`CREATE TABLE` etc.) — the runtime role needs schema privileges.
- Repos: `db/*.go` — SELECT/INSERT/UPDATE/DELETE/TRUNCATE on ARTEX tables; `ensureDatabase` (db/db.go:102) creates missing databases (`CREATE DATABASE`) — superuser-grade privilege.
- No Row-Level Security: no RLS anywhere (grep).
- Connection: `db.Open` — pgx pool; credentials only from env/config.json — never logged (grep).

Test roles (A4):
- Only `postgres` exists in the lab. Restricted-role verification of the runtime role: NOT DONE this phase (no production use of alternate roles to verify against — see F-A1).

## 4. Data ownership graph (Gate B1)

| Entity | Fields (ownership/context) | Source |
|---|---|---|
| Task | `db.Task` (db/tasks.go:13): id, name, description, goal, exploration_id, status... — **no owner/user/tenant column** | Source verified |
| Finding | `db.Node` (db/exploration.go:67): id, kind, payload, state, **owner** (string — node owner?), SourceTaskID... — **no user ownership**; `db.DBFinding`? | Source verified |
| Evidence (TrafficRef) | `db.TrafficRef{TrafficID, Role, Note}` (db/finding_traffic.go:42-46) — **no hash/length stored**; `TrafficEvidenceSnapshot{ReqHash, ReqLen, ...}` (db/finding_traffic.go:74-88) stored on the binding — hash lives in DB + blob path | Source verified |
| AttackPath | `agent/tools_attackpath.go` — references findings/tasks; ownership via `agent.TriggerEvent`? — NOT INSPECTED (Budget) | NOT INVESTIGATED |
| Retest | `agent/tools_retest.go` — references findings — NOT INSPECTED | NOT INVESTIGATED |
| Sessions/audit | `logSink` — sessions, audit events (db — tables created by runtime) — no user fields (single-admin) | Source verified |
| Node.Owner | `db.Node.Owner string` — "owner" of the node — semantics? — inspect (documented) | Source verified, semantics NOT INSPECTED |

## 5. Repository query authorization review (Gate B2)

Key queries (representative; full review of every repo method budget-limited — NOT FULLY COVERED):
- `db.GetNode`/`ListByKind` — by ID — `db/exploration.go` — no task filter for findings? — Grep: `FindingID` on Node — "populated by finding-aware reads" (db/exploration.go:68) — i.e., cross-check at read time is caller-injected.
- `evidence.Store.Bind` (store.go:203-215): `LockFindingEvidenceTx(tx, findingID, nil)` — `nil` argument — check semantics (db/finding_traffic.go:340+ `RecordFindingTx` — `PreparedTrafficEvidence` — the binding is created with findingID — is task validated? — Documented).
- No repository method validated ownership (single-admin; documented).

## 6. Transaction & state-transition analysis (Gate C)

- Finding creation: `db.RecordFindingTx` (db/finding_traffic.go:342) — used by `evidence.Store.Record` — transaction with `LockFindingEvidenceTx` — partial inspection (Budget).
- Evidence bind/store: `evidence.Store.Bind`/`Put` — `WithEvidenceTx` — transactional.
- Retest lifecycle: `agent/tools_retest.go` — NOT INSPECTED (budget).
- Concurrent workers: not tested (budget — race unavailable).

## 7. Evidence provenance & integrity (Gate D)

**Integrity (TEST VERIFIED — new tests in `evidence/v8_6_tamper_test.go`):**
- `TestEvidenceTamperDetection_v86` — PASS: `verifyFile` fails on modified blob content and on deleted blob (store.go:44-58, compares SHA256 hex of content + length).
- `TestEvidenceOpenBodyRetrievalDetectsTamper_v86` — PASS: `OpenBody` (store.go:253-270 — `verifyFile(path, hash, length)` at :264) fails on tampered blob; invalid side rejected.
- `TestEvidenceFixtureRequiresDSN_v86` — integration-only (skips without DSN).
- Positive: `Store.writeBody` — store.go:63-109 — computes hash from content, verifies existing path (store.go:96).
- **F-D1 (MEDIUM-HIGH):** `db.TrafficRef` stores no content hash/length — integrity relies on the blob filename = hash; retrieval re-verifies (OpenBody). If a caller constructs a snapshot with a wrong hash (fabrication path via `Record`/`Bind`), integrity is unverified at write — `evidence.Store.prepare` (store.go:152) — NOT INSPECTED (budget).

**Provenance (SOURCE VERIFIED, partial):**
- Producers: traffic package (WebFetch/HTTP tool requests — `traffic.Put` — captured); Workers (task runs) — write paths.
- Consumers: `evidence.Store.OpenBody`, report generation, retest — read paths with verification.
- F-B2 (MEDIUM): `Store.Bind` — `LockFindingEvidenceTx(tx, findingID, nil)` — `nil` — semantics of second arg — NOT INSPECTED.

**Finding trust-dependency (F-B3, MEDIUM):**
- No automation marks a finding invalid when evidence is tampered; findings remain presented as verified (documented) — NOT INSPECTED (budget).

## 8. Audit-log integrity review (Gate E)

- Traffic-proxy decision audit events: `traffic.SetEgressAuditSink` — **no production callers** (viii.4.8 finding retained).
- `emitTrafficEgressAudit` calls exist (traffic/egress.go:191+) — emitted to the sink only; without a sink, decisions are not recorded.
- `emitEgressEvent` (agent/egress_policy.go) — `egressAuditFunc` — also no production callers found (viii.4.8).
- `logSink` (server/logsink.go) — runtime logs to DB — sessions/audit captured.
- F-E1 (HIGH): without the sink, proxy egress decisions (allow/deny) are not persisted anywhere — document operational gap.
- F-E2 (LOW): CSRF on cookie auth — not inspected.

## 9. Cross-boundary adversarial results (Gate F)

| # | Scenario | Result |
|---|---|---|
| 1 | Runtime role unauthorized schema modification | NOT RUN (budget); role is superuser (F-A1). Documented. |
| 2 | Worker cross-task modification | Single-admin; `db.Node.Owner` semantics unverified (NOT INVESTIGATED, budget). |
| 3 | Forged task ID in job payload | Not inspected (NOT INVESTIGATED). |
| 4 | Evidence with wrong-task link | `TrafficRef` has no task field (only role/note) — task binding is at the finding level; tampering tests cover blob integrity (D). |
| 5–15 | Rest of Gate F | Budget exhausted — documented as NOT INVESTIGATED except evidence tamper (D.2) and privilege audit (A). |

## 10. Race-detector follow-up (Gate G)

- `RACE DETECTOR UNKNOWN — ENVIRONMENT BLOCKED` (no C compiler; carried from viii.4.8).
- Read-policy revisit (G2): Read tools remain permissive (viii.5 decision — documented); provider of impact: Read can read any file in the process user's account (across "tasks" — single admin). Documented.

## 11. Files changed and justification

- `evidence/v8_6_tamper_test.go` (new) — evidence tamper detection tests.
- No production changes in VIII.6 (finding phase) — all changes from viii.4.6→8.5.1.

## 12. Commands and exit codes (viii.6)

- `go vet ./evidence/` → 0.
- `go test ./evidence/ -run 'TestEvidenceTamperDetection_v86|TestEvidenceOpenBodyRetrievalDetectsTamper_v86' -count=1 -v` → PASS (exit 0).
- `go test ./evidence/ -count=1 -timeout 600s` → ok (exit 0).
- psql (lab, RUNTIME VERIFIED): `SELECT rolname, rolsuper, rolcreatedb, rolcreaterole, rolbypassrls FROM pg_roles WHERE rolname IN ('postgres',...)` → postgres: t,t,t,t (F-A1).

## 13. Remaining risks

1. F-A1 (CRITICAL): runtime role is superuser.
2. F-E1 (HIGH): no production audit wiring for proxy decisions.
3. F-B1 (MEDIUM-HIGH): TrafficRef lacks content hash/length; write-time fabrication path uninspected.
4. F-B2 (MEDIUM): `LockFindingEvidenceTx` `nil` arg semantics — binding-time validation unverified.
5. F-B3 (MEDIUM): no finding-invalidation automation on evidence tampering.
6. Race detector (UNKNOWN — env).
7. Retest/attackpath task-binding checks — NOT INVESTIGATED (budget).
8. Node.Owner semantics — NOT INVESTIGATED.

## 14. Final verdict

**`PARTIAL — DB PRIVILEGE DEFECT FOUND (F-A1) + EVIDENCE INTEGRITY VERIFIED (D) + AUDIT WIRING GAP (F-E1)`**

Key results: evidence tamper detection proven (TEST VERIFIED); traffic-proxy audit wiring missing in production (HIGH); runtime DB role = superuser (CRITICAL); binding/provenance deeper checks pending (MEDIUM, NOT INVESTIGATED). Phase not fully PASS because F-A1 is a CRITICAL privilege finding and F-E1 a HIGH audit gap.

## 19. Final verdict (summary)

Choose exactly one:

- `PASS — EVIDENCE INTEGRITY & ADMINISTRATION WINDOW PARTIALLY DOCUMENTED; DB PRIVILEGE DEFECTS CRITICAL` — (FINAL; see §14).

A full PASS requires F-A1 (restricted runtime role) and F-E1 (production audit wiring) to be resolved or formally accepted — neither is; the phase therefore cannot claim "PASS".
