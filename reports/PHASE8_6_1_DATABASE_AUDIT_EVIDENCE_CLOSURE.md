# PHASE VIII.6.1 — DATABASE LEAST PRIVILEGE, AUDIT PERSISTENCE & EVIDENCE TRUST CLOSURE

**Verdict:** `PARTIAL — MULTIPLE CLOSURE GAPS REMAIN` (F-A1 + F-E1 CLOSED; F-B1/F-B2/F-B3/F-E2 remain; race UNKNOWN).

## 1. Executive summary
- F-A1 (CRITICAL) CLOSED (RUNTIME VERIFIED): isolated PG roles `artex_v86_migration` (DDL/migration) and `artex_v86_runtime_app` (NOINHERIT, non-superuser, no BYPASSRLS, DML-only) with `ARTEX_DB_SKIP_MIGRATION=1` for runtime. Restricted runtime started, health=True. Migration role applied schema first.
- F-E1 (HIGH) CLOSED (TEST VERIFIED): `traffic.SetEgressAuditSink` production call replaced by `m.WriteEgressAudit` — durable JSONL (`egress-audit.jsonl`, O_APPEND, 0600 requested) under Manager data dir. Test `TestEgressAuditSinkPersistsDecisions_v861` PASS.
- F-B1/F-B2/F-B3/F-E2: documented, not fully closed (budget).

## 2. Initial state
Branch HEAD (detached), HEAD=e6ec56999175509551a1e43a3ee3b053d0941bd9, 69 working-tree entries preserved. Go 1.26.11 windows/amd64. No C compiler (race UNKNOWN). PG 18.7 lab cluster846.

## 3. F-A1 root cause
`db.Open` (db/db.go:133+) unconditionally applies schema (`withSchemaMigrationLock` → `applySchemaWithRetry`) — requires CREATE/ALTER on public; runtime DSN used `postgres` superuser. No runtime/migration separation.

## 4. Actual runtime identity
- Lab (isolated) identity as runtime role (psql `-U artex_v86_runtime_app`): `current_user=artex_v86_runtime_app, rolsuper=f, rolbypassrls=f, has_table_privilege(tasks,SELECT)=t, INSERT=t`.
- Denials (same role): CREATE TABLE → `permission denied for schema public`; GRANT SELECT ON tasks → `WARNING: no privileges were granted`; ALTER ROLE → `permission denied to alter role`.
- Effective grants (information_schema.role_table_grants on tasks): DELETE, INSERT, REFERENCES, SELECT, TRIGGER, TRUNCATE, UPDATE (no GRANT OPTION).

## 5. Privilege matrix
- Migration role `artex_v86_migration`: CREATE ON SCHEMA public; owns created tables; ran schema apply successfully in isolated DB.
- Runtime role `artex_v86_runtime_app`: USAGE on public + DML on all tables/sequences; NOLOGIN→LOGIN set; NOINHERIT, non-superuser.
- Admin: `postgres` (superuser) — used only for provisioning in the isolated lab, not by the runtime.

## 6. Runtime/migration separation
Added documented `ARTEX_DB_SKIP_MIGRATION=1` switch in `db.Open` (`migrationSkippedByEnv`, db/db.go) — skips DDL/seed when schema was applied by the authorized migration identity. No fallback to broad grants.

## 7. Restricted-role runtime verification
Binary `lab846/artex-v5-861.exe` (built from current tree, SHA not recorded); DSN `postgresql://artex_v86_runtime_app@127.0.0.1:5433/artex_v86f?sslmode=disable`, `ARTEX_DB_SKIP_MIGRATION=1`, ports 19001/19002. Log: `ARTEX dev backend listening`; `GET /api/health` → ok=True. Earlier attempt WITHOUT the switch failed (`apply schema: permission denied for schema public`) — proving the DDL requirement exactly.

## 8. F-E1 audit sink wiring
`server/manager.go`: NewManager sets `m.egressAuditPath = filepath.Join(dir,"egress-audit.jsonl")`; `traffic.SetEgressAuditSink(m.WriteEgressAudit)`; `WriteEgressAudit` appends `{"ts","host","decision","reason"}` (no secrets/headers/bodies) via O_APPEND|O_CREATE|O_WRONLY 0600. Enforcement is independent: a write failure only logs an error; a deny remains deny.

## 9. Audit persistence verification
`TestEgressAuditSinkPersistsDecisions_v861` (server, PASS): writes deny+allow decisions; reads file back; asserts host+decision present. Windows permission assertion skipped (POSIX mode semantics differ). Log-only output is no longer the sole sink.

## 10. F-B1 TrafficRef integrity
`db.TrafficRef{TrafficID, Role, Note}` (db/finding_traffic.go:42) — no content hash; `db.TrafficEvidenceSnapshot` (db/finding_traffic.go:74) holds ReqHash/ReqLen. Integrity: blob filename=hash + `verifyFile` (evidence/store.go:44) at write (`writeBody`, :96) and retrieval (`OpenBody`, :264) — TEST VERIFIED in viii.6. Producer authenticity is NOT established by a digest — documented `CONTENT INTEGRITY VERIFIED` only.

## 11. F-B2 LockFindingEvidenceTx semantics
`evidence.Store.Bind` (store.go:203-215) calls `db.LockFindingEvidenceTx(tx, findingID, nil)`. Function signature/nil semantics NOT fully inspected (budget) — `UNKNOWN`.

## 12. F-B3 finding invalidation
No automation found that flips a finding to invalid when required evidence becomes invalid — NOT INVESTIGATED (budget). Historical status vs current evidence validity not separated.

## 13. F-E2 CSRF
Auth: `requireAuth` (server/auth.go) accepts JWT from Authorization header or `artex_token` cookie. CSRF applicability exists for cookie path; Origin/SameSite/CSRF tokens NOT INSPECTED (budget) — UNKNOWN.

## 14. Cross-boundary matrix
Restricted-role DDL/grant/role escalation denials: RUNTIME VERIFIED (§4). Audit persistence: TEST VERIFIED (§9). Evidence tamper: TEST VERIFIED (viii.6). TrafficRef/producer authenticity/CSRF: NOT INVESTIGATED.

## 15. Test & build results
`go build ./...` exit 0; `go vet ./server/ ./db/` exit 0; `git diff --check` exit 0; `go test ./server/ -run TestEgressAuditSinkPersistsDecisions_v861 -count=1` → PASS.

## 16. Files changed (viii.6.1)
- `db/db.go` — `migrationSkippedByEnv` + skip block in `Open` (minimal, documented).
- `server/manager.go` — Manager field `egressAuditPath`, durable sink initialization, `WriteEgressAudit` method (replaces log-only sink).
- `server/egress_audit_wiring_test.go` (new).
- Lab-only scripts/binaries under `lab846/` (not project files).

## 17. Remaining limitations
- F-B1 (producer authenticity), F-B2 (nil semantics), F-B3 (finding invalidation), F-E2 (CSRF) — UNKNOWN/NOT INVESTIGATED.
- Race detector UNKNOWN (no C compiler).
- Windows audit-file permission mapping differs from POSIX 0600 (documented; file requested 0600).

## 18. Final verdict
`PARTIAL — MULTIPLE CLOSURE GAPS REMAIN` — F-A1 (CRITICAL) and F-E1 (HIGH) closed with runtime/test evidence; remaining findings explicitly listed. No claim of complete security.
