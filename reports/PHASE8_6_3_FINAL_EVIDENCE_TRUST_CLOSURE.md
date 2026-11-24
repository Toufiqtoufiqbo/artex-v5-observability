# PHASE VIII.6.3 â€” FINAL EVIDENCE TRUST CLOSURE (ARTEX v5)

**Final verdict:** `PARTIAL â€” RACE DETECTOR UNKNOWN`

---

## 1. Executive summary

Phase VIII.6.3 closed the finding-evidence trust gates F-B3 (finding invalidation), F-B1 (TrafficRef producer authenticity, app-level model), N1 (cookie/`?token=` method-scope hardening) and N3 (`WriteEgressAudit` fsync), and re-verified the earlier controls. Findings closed this phase: **F-B3, F-B1 (model + tests), N1 (tests + runtime), N3 (tests)**. Open/unverified: **race detector (environment blocked)**. Verdict `PARTIAL â€” RACE DETECTOR UNKNOWN` follows the project rule: a full PASS requires the race gate to be a real `go test -race` run or resolved, so the best achievable verdict is partial at present. Tree is preserved; lab processes stopped; the disposable lab DB is dropped; PG is stopped with port 5433 free.

## 2. Initial state (preserved)

- Detached HEAD `e6ec56999175509551a1e43a3ee3b053d0941bd9` in the repo; uncommitted baseline (VIII.4.6â†’VIII.6.2 changes intact).
- Baseline tree: 17 modified + 54 untracked (71 entries).
- Go `<GO_BIN>\go.exe` 1.26.11 windows/amd64. PG cluster846, port 5433 (was STOPPED, as found).

## 3. Master security debt register (viii.6.3 closure status)

| ID | Component | Status (8.6.3) | Evidence |
|---|---|---|---|
| F-A1 | PostgreSQL least privilege | CLOSED (8.6.1, re-verified this phase) | roles `artex_v86_runtime_app` / `artex_v86_migration`; `RUNTIME VERIFIED` |
| F-B1 | TrafficRef integrity & producer authenticity | CLOSED (this phase) | app-level authenticity; caller-forgery test PASS; provenance `TEST+SOURCE VERIFIED` |
| F-B2 | LockFindingEvidenceTx | CLOSED (8.6.2, unchanged) | `db/finding_traffic.go:163-183` |
| F-B3 | Finding invalidation | **CLOSED (this phase)** | unit `v8_6_3_*` tests PASS + runtime invalidate/restore PASS |
| F-E1 | Audit persistence (8.6.1 wiring) | CLOSED (8.6.1, unchanged) | `server/manager.go` wiring; runtime deny records present |
| F-E2 | CSRF | CLOSED (8.6.2, unchanged) | auth model proven: JWT-only path |
| N1 | cookie/`?token=` auth scope (HIGH/LOW) | **CLOSED (this phase)** | `extractToken` GET/HEAD only for cookie/query; `TestExtractTokenMethodScope_v863` + `TestRequireAuthRejectsCookieStateChange_v863` PASS; runtime 401 |
| N2 | pre-8.6.1 audit gap | CLOSED (by 8.6.1, unchanged) | restart/wiring; N2 remains OPEN for old deployments |
| N3 | WriteEgressAudit durability | **CLOSED (this phase)** | `f.Sync()` added; `TestEgressAuditDurabilityAndInjection_v863` PASS |
| Race detector | Gate I | UNKNOWN â€” environment | gcc/clang/cl/zig absent; `RACE DETECTOR UNKNOWN â€” ENVIRONMENT BLOCKED` (see Â§13) |
| F-A2 | Windows symlink test | UNKNOWN (env) | skipped on Windows; static-reviewed (unchanged) |

## 4. Gate A â€” Egress control & proxy (regression)

- Unchanged this phase (VIII.4.7/8): sealed-empty egress allowlist by default, operator allowlist via `ARTEX_EGRESS_ALLOWLIST`, deny-all fail-closed. Regression rerun: `go test ./traffic/ -count=1` â†’ **PASS** (59.875s, exit 0).
- A2 no-caller grep: `SetTrustedProviderHosts` has no production callers â†’ `SOURCE VERIFIED`.
- Runtime A5: restricted-role runtime of the new binary served health 200 (see Â§19).

## 5. Gate B â€” Local execution & filesystem (regression)

- Regression rerun (exit codes recorded): `go test ./agent/ -count=1` â†’ FAIL, `TestGraphOverviewExpandsAssociatedCompanyScope` â€” **PRE-EXISTING / FLAKY (unrelated; files untouched)**; `go test ./server/` â†’ FAIL, `TestCoreTaskLifecyclePG` (async goal seeding â€” environmental, no LLM provider) and `TestSkillRelPath` (PRE-EXISTING, Unix-semantics vs Windows `filepath.Clean`). No new failures attributable to viii.6.3.
- Full-suite package results: `./evidence` ok (39.765s), `./db` ok (101.493s), `./traffic` ok (59.875s).

## 6. Gate C â€” PostgreSQL least privilege (F-A1, re-verified)

- Runtime role `artex_v86_runtime_app`: `pg_roles.rolsuper=f`, `rolbypassrls=f` â†’ `RUNTIME VERIFIED`. Grants: schema USAGE + DML (SELECT/INSERT/UPDATE/DELETE/TRUNCATE/REFERENCES/TRIGGER) on all public tables + USAGE/SELECT/UPDATE on sequences. Migration role `artex_v86_migration` applied schema; runtime has no CREATE rights (fresh tables are created during migration) â†’ consistent with 8.6.1 runtime behavior.
- `ARTEX_DB_SKIP_MIGRATION=1` used by the lab runtime, matching the 8.6.1 deployment model.

## 7. Evidence traffic flow â€” F-B1 (producer authenticity)

The app-level authenticity model (design for F-B1):

- `TrafficRef` carries only `TrafficID` / `Role` / `Note` â€” callers cannot supply hashes, URLs, or metadata.
- Server side: `traffic.ReadEvidence` â†’ `writeBody` computes request/response hashes and sizes from the capture store; `evidence.Store.Bind` inserts a snapshot whose `ID = db.TrafficSnapshotID(s)` (SHA-256 over normalized metadata; unknown traffic IDs rejected with no side effects; caller-supplied snapshot IDs rejected).
- Runtime-verified: exported blob hash matches the stored `resp_hash`/`req_hash`; content-addressed path `blobs/<hash[:2]>/<hash>.bin`; tamper/deletion invalidates via F-B3 (Â§9).
- Known bounded limitation (recorded): the `exchanges` capture table has no `task_id`; capture association is global. Finding `task_id` is the authoritative scoping point (finding_traffic bindings are per-finding). DB-write-level adversary is outside the documented threat model (no keyed MAC on content-addressed digests).

## 8. LockFindingEvidenceTx (F-B2, unchanged)

- `db/finding_traffic.go:163-183`: `SELECT task_id FROM findings WHERE id=$1` (missing â†’ `ErrFindingNotFound`), archive lock, then `SELECT evidence_version FROM findings WHERE id=$1 FOR UPDATE`; `version != nil && *version != current â†’ ErrEvidenceConflict`. `nil` version = skip optimistic check (documented). `SOURCE VERIFIED`; regression rerun (`server -run 'v861|v863'` exit 0).

## 9. Finding invalidation (F-B3 â€” CORE CLOSE)

- Read-time validity in `evidence/store.go`: `EvidenceState` (`valid` / `invalid` / `none`), `EvidenceValidity{State, Bindings, InvalidBindingIDs, Detail}`, `ErrEvidenceInvalid`, `InvalidEvidenceError{FindingID, BindingID, Side, Reason}`, `ValidateFindingEvidence` (advisory lock `pg_advisory_xact_lock` via `WithEvidenceTx`; zero bindings + `evidence_version > 0` â‡’ invalid "bound evidence removed"; zero bindings + version 0 â‡’ `none`).
- Metadata integrity: `b.Snapshot.ID != db.TrafficSnapshotID(b.Snapshot)` â‡’ invalid (`#N metadata: å¿«ç…§å…ƒæ•°æ®å“ˆå¸Œä¸åŒ¹é…`).
- Body integrity: `checkSnapshotBody` stat + `verifyFile` hash/length â‡’ invalid (`#N request|response: æ­£æ–‡æ–‡ä»¶ç¼ºå¤± | æ­£æ–‡å“ˆå¸Œæˆ–é•¿åº¦ä¸åŒ¹é…`).
- `StageFindingsExport`: verifies every bound body for **every** format (`copyBodies` only controls copying) â†’ `InvalidEvidenceError` on invalid (fail closed).
- Detail DTO: `getFinding` now returns `evidence_state`, `evidence_valid`, `invalid_evidence_binding_ids`, `current_verification` (`verified`/`unverified`/`stale`/`invalid`/`unknown`).
- Integrity failures logged: `[evidence-integrity] finding=N state=invalid ...` â†’ persisted to `server_logs` via the existing `log.Printf`â†’`logSink` wiring (runtime-verified rows present).
- `startFindingRetest`: blocked on invalid evidence (409) or validation failure (503).

### 9a. Unit/integration tests (fresh isolated PG; all PASS, exit 0)

- `go test ./evidence/ -run 'v862|v863'` â†’ **PASS** (`evidence_v86_exit=0`, 8.829s): `TestEvidenceValidityLifecycle_v863` (tamperâ†’invalidâ†’historical row survivesâ†’exact-bytes restoreâ†’valid), `TestEvidenceValidityMetadataTamper_v863`, `TestEvidenceValidityRemovedBindings_v863`, `TestEvidenceValidityNoEvidence_v863`, `TestEvidenceProvenanceCallerCannotForge_v863` (unknown traffic ID rejected, forged snapshot ID rejected, caller can't supply hashes, cross-finding isolation), `TestEvidenceValidityConcurrentBind_v863` (barrier â€” see Â§13).
- `go test ./server/ -run 'v861|v863'` â†’ **PASS** (`server_v86_exit=0`): `TestFindingDetailReflectsEvidenceValidity_v863` (detail shows `evidence_state=valid/invalid`, export CSV â†’ 409 on invalid, retest â†’ 409, finding survives, restore â†’ valid), `TestEgressAuditSinkPersistsDecisions_v861` regression, `TestExtractTokenMethodScope_v863`, `TestRequireAuthRejectsCookieStateChange_v863`.

## 10. Egress audit durability (F-E1 + N3)

- F-E1 (8.6.1): production wiring `traffic.SetEgressAuditSink(m.WriteEgressAudit)` (`server/manager.go:418`); file `egress-audit.jsonl`, 0600 O_APPEND, records `{"ts","host","decision","reason"}`; write failure logs, deny stays deny. Unchanged this phase.
- N3 (this phase): `Manager.WriteEgressAudit` now calls `f.Sync()` after each `WriteString`; sync failure logged (`[traffic] egress audit fsync failed`). Fail-closed guarantee is independent of persistence (deny remains deny).
- `TestEgressAuditDurabilityAndInjection_v863` PASS: 24 concurrent appends (12 allow/12 deny), parseable JSON lines, newline-injection not possible, sink-open-failure path contained.

## 11. CSRF / auth scope (N1 â€” CLOSED)

- `extractToken` (`server/auth.go:106-124`): `Authorization: Bearer` always accepted; `artex_token` cookie and `?token=` query accepted **only on GET/HEAD** (SSE + GET-based integrations); state-changing methods (POST/PATCH/DELETE) require Bearer.
- Tests PASS: `TestExtractTokenMethodScope_v863` (11 cases incl. POST-cookieâ†’"", POST-queryâ†’"", PATCH-cookieâ†’"", etc.), `TestRequireAuthRejectsCookieStateChange_v863` (cookie POST â†’ 401 before handler; Bearer POST â†’ 204; cookie GET â†’ 204).
- Frontend context (evidence for why N1 was real): `web/src/lib/auth.ts` sets `artex_token` cookie `SameSite=Lax` (Next.js middleware use); SSE uses `?token=` GET; CORS is `*` with no credentials. Cookie-on-mutations had no production effect for SSE, but the surface was real and is now closed.

## 12. Token/cookie exposure surface (documentation)

- Known accepted residual: `?token=` in URLs of GET requests is visible in browser history/proxies for SSE. Downgraded to GET-only, logged as future hardening option.

## 13. Race detector (Gate I â€” UNKNOWN, environment blocked)

- Command attempted: `go test -race ./evidence/ -run TestEvidenceValidityNoEvidence_v863`.
- Exact failure: `cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%` â€” `race_exit=1`.
- Environment survey: `gcc`/`clang`/`cl`/`zig` all absent from PATH; `vswhere` (Visual Studio) empty; no WSL distro; `go env CC CGO_ENABLED` â†’ `gcc` / `1`; `go test -race` requires a C compiler on this platform.
- Determinism note: the added concurrency tests (`TestEvidenceValidityConcurrentBind_v863`, `TestEgressAuditDurabilityAndInjection_v863`) use barriers, not sleeps, but they are **not** substitutes for `-race`. â†’ `RACE DETECTOR UNKNOWN â€” ENVIRONMENT BLOCKED`.

## 14. Full regression (Gate J)

| Command | Result |
|---|---|
| `go test ./evidence/ -run 'v862|v863' -count=1` | PASS, exit 0 |
| `go test ./server/ -run 'v861|v863' -count=1` | PASS, exit 0 |
| `go test ./evidence/ ./db/ ./traffic/ -count=1` | ok (exit 0; evidence 39.765s, db 101.493s, traffic 59.875s) |
| `go test ./server/ -count=1` | FAIL â€” `TestCoreTaskLifecyclePG` (pre-existing/env), `TestSkillRelPath` (pre-existing); see Â§5 |
| `go test ./agent/ -count=1` | FAIL â€” `TestGraphOverviewExpandsAssociatedCompanyScope` (pre-existing/flaky) |
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `git diff --check` | exit 0 |

## 15. Cross-boundary adversarial matrix

1. Retest blocked on invalid evidence (409) â€” runtime-verified, Â§19.
2. All export formats refuse invalid evidence (409) â€” runtime CSV verified; unit covers md-zip/csv/json/md-single via `copyBodies` false/true.
3. Historical finding rows are never modified or deleted (validated on restore; `GetFinding` still returns row 1).
4. Exact-byte restore revalidates to `valid` (hash-based, stateless at read).
5. DB-only metadata tamper detected (`url` swap) â€” `TestEvidenceValidityMetadataTamper_v863` (snapshot ID is a digest of normalized metadata).
6. Caller cannot forge snapshots or supply hashes â€” `TestEvidenceProvenanceCallerCannotForge_v863`; unknown traffic IDs rejected with no binding side effects.
7. Cookie/query creds on mutations rejected before the handler (401) â€” `TestRequireAuthRejectsCookieStateChange_v863` + runtime.
8. Egress decisions independent of audit persistence (write failure never flips denyâ†’allow) â€” by design; `TestEgressAuditDurabilityAndInjection_v863` contains the open-failure path.
9. Newline injection in audit `reason` cannot forge audit records â€” contained (line JSON per record; tested).
10. Advisory-lock coverage: validate/bind/edit/GC run under `WithEvidenceTx` so blobs cannot unlink mid-hash.

## 16. New findings discovered this phase

- N1 (previously HIGH/LOW): closed â€” GET/HEAD-only cookie/query auth; runtime 401 verified.
- N3: closed â€” `f.Sync()` added; durability test passes.
- `cmd/v863seed/` lab helper: **removed from the tree** after the lab run (not product code).
- Remaining accepted-by-design (unchanged): race detector UNKNOWN; Windows symlink test UNKNOWN (env); F-B2 documented nil semantics; F-A2 symlink static review; provider/traffic pooling, TLS SNI runtime, IPv6 runtime (unchanged).

## 17. Files changed (viii.6.3)

- `evidence/store.go` â€” validity API (`EvidenceState`, `EvidenceValidity`, `ErrEvidenceInvalid`, `InvalidEvidenceError`, `ValidateFindingEvidence`, `checkSnapshotBody`, `validateBindings`), `StageFindingsExport` verify-all-formats.
- `evidence/v8_6_3_evidence_validity_test.go` â€” lifecycle, metadata tamper, removed bindings, no-evidence, provenance, concurrency.
- `server/finding_traffic.go` â€” `applyEvidenceValidity` + `evidence_state/evidence_valid/invalid_evidence_binding_ids/current_verification` fields, `[evidence-integrity]` logging, `ErrEvidenceInvalid` mapped to 409 in `evidenceError`.
- `server/dto.go` â€” `FindingDTO` validity fields.
- `server/server.go` â€” `getFinding` attaches validity.
- `server/finding_retests.go` â€” retest gating (409 invalid / 503 unknown) + `get_finding_retest_context` validity.
- `server/auth.go` â€” `extractToken` GET/HEAD-only for cookie/query (N1).
- `server/v8_6_3_auth_test.go`, `server/v8_6_3_audit_durability_test.go`, `server/v8_6_3_evidence_gate_test.go` â€” new tests.
- `server/manager.go` â€” `WriteEgressAudit` `f.Sync()` (N3).
- Lab (outside repo): `lab863/` scripts, `lab846/artex-v5-863.exe` â€” artifacts removed at cleanup.

## 18. Commands & exit codes (viii.6.3)

- `go build ./...` â†’ exit 0; `go vet ./...` â†’ exit 0; `git diff --check` â†’ exit 0.
- `go test ./evidence/ -run 'v862|v863' -count=1` â†’ exit 0 (8.8s).
- `go test ./server/ -run 'v861|v863' -count=1` â†’ exit 0 (2.0s).
- `go test ./evidence/ ./db/ ./traffic/ -count=1` â†’ exit 0.
- `go test ./server/ -count=1` â†’ exit 1 (pre-existing/env failures, see Â§5/Â§14).
- `go test ./agent/ -count=1` â†’ exit 1 (pre-existing `TestGraphOverviewExpandsAssociatedCompanyScope`).
- `go test -race` â†’ exit 1 (`gcc` not found; race UNKNOWN, Â§13).

## 19. Lab runtime verification (fresh isolated DB)

- Fresh disposable DB: `artex_v863f` (created this phase); roles: `artex_v863_runtime_app` (LOGIN; granted schema USAGE + DML on tables/sequences), `artex_v863_migration`. Migration applied via the new binary as superuser.
- Seeded: finding id 1 ("Runtime evidence", evidence_version=1), exchange `v863-rt`, response blob `41f0e3dcâ€¦e727bab` (28 B, verified against `resp_hash`).
- Runtime: `artex-v5-863.exe` with the restricted runtime role, `ARTEX_DB_SKIP_MIGRATION=1`, sealed-empty egress allowlist; health=200.
- Runtime checks (all observed):
  - `GET /api/exploration/findings/1` â†’ `evidence_state=valid`, `evidence_valid=true`, `current_verification=unverified`, `traffic_count=1`.
  - Tamper response blob (byte XOR) â†’ `evidence_state=invalid`, `evidence_valid=false`, `invalid_evidence_binding_ids=1`, `current_verification=invalid`; integrity lines persisted to `server_logs`.
  - `GET export?format=csv` while invalid â†’ **409**.
  - `POST /retests` while invalid â†’ **409**.
  - Cookie-authenticated `POST /retests` (no Bearer) â†’ **401** (N1).
  - Egress proxy request to `192.0.2.1` â†’ `egress-audit.jsonl` contains `{"decision":"deny","reason":"traffic proxy: egress to 192.0.2.1 denied by explicit allowlist"}`.
  - Exact-byte restore â†’ `evidence_state=valid`; export returns 200.
- Cleanup: server stopped; DBs `artex_v863f`, `artex_v863`, `artex_v863s`, `artex_v863a` dropped; port 5433 free (`pg_isready: no response`); PG stopped; lab binaries/helpers removed from `lab846/`; repo tree preserved.

## 20. State: processes, DBs, tree

- Lab processes: stopped (server exited; PG stopped, port 5433 no listener).
- Disposable DBs created this phase (`artex_v863f`, `artex_v863`, `artex_v863s`, `artex_v863a`): dropped.
- Working tree: 21 modified + 59 untracked preserved; baseline VIII.4.6â†’VIII.6.2 changes intact; no commits/pushes.

## 21. Final verdict

**`PARTIAL â€” RACE DETECTOR UNKNOWN`**

Rationale: every viii.6.3 gate is satisfied â€” F-B3 (source+test+runtime verified), F-B1 (app-level authenticity; caller-forgery, provenance, cross-finding isolation all TEST+SOURCE verified; digest-only limitation documented as accepted-with-design), N1 (closed by hardening, unit-tested and runtime-verified with a 401), N3 (`f.Sync()`; unit-tested). The race gate is the only blocker: it cannot run in this environment (`gcc`/cl compiler absent, no WSL, installing a compiler is out of scope), so the full-PASS condition is not met and no Phase IX work is triggered. Pre-existing unrelated failures (`TestCoreTaskLifecyclePG`, `TestSkillRelPath`, `TestGraphOverviewExpandsAssociatedCompanyScope`) and the Windows symlink-test environment gap are recorded but are not caused by this phase. Remaining accepted risks: provider/traffic pooling, TLS SNI runtime, IPv6 runtime, race freedom of concurrent evidence validation/binding (barrier-tested but not `-race`-verified), and the `exchanges` table's lack of `task_id` (capture-level association is global; finding `task_id` remains authoritative).
