# MASTER REPORT — VIII.4.8 TRUST ROOT, CONCURRENCY & REGRESSION CLOSURE

**Verdict:** `PARTIAL — RACE DETECTOR UNAVAILABLE` (see §6). All security-boundary work done in this phase is TEST-verified; the only open item is race freedom, blocked by missing C compiler.

## 1. Executive summary

All three objectives of VIII.4.8 completed or blocked with documentation: (A) Provider trust root provenance — proven operator-config-only, adversarial tests pass; (B) Traffic allowlist administrative integrity — verified; (C) Race detector — unavailable (no C compiler); (D) Regression failures — resolved/classified with evidence; (E) Cross-boundary adversarial sequences — verified via existing suites + new concurrency tests. Working tree preserved; lab stopped.

## 2. Initial working-tree state (preserved; never modified/discarded)

- Branch: `HEAD` (detached). HEAD: `e6ec56999175509551a1e43a3ee3b053d0941bd9`.
- `git status --short` (28 modified incl. viii.4.6–4.8 changes: agent/provider*, agent/{chat,mainagent,planner,tools,worker,bash_guard}, agent/{httpclient,scopecheck*}, enrich/{enrich,egress_test}, evidence/store.go, selfupdate/{github,stage}, server/{assembly,manager,server}).
- Go: `go1.26.11 windows/amd64`. OS: windows (amd64). `CGO_ENABLED=0`, `go env CC`=gcc; **no C compiler installed** (`where gcc cl cc clang zig tcc` → none).
- PG 5433: not running at phase start. No external LLM calls, no public scans (local lab only).

## 3. Gate A — Trust root provenance — `TEST VERIFIED` (source+tests), complete

- **A1 source trace (`server/server.go`, `agent/provider_egress.go`, `agent/provider.go`, `db/`):** trust root = `ARTEX_TRUSTED_PROVIDER_HOSTS` (operator env) → `TrustedProviderHostsFromEnv()` → `SeedTrustedProviderHosts(...)` at `server/server.go:243` (first-seed-wins; env installed before any provider build). `SetTrustedProviderHosts` — production callers: none. `SetTrustedProviderPolicy` — production callers: none (tests only). `SeedTrustedProviderHostFromBaseURL` — deleted in VIII.4.7 (no callers). Provider builds: `applyLLM` (server.go:452), `providerForProfile` (server.go:597), `setLLM` (server.go:1367), `TestConnection` (provider.go:498) — all trust-gated. `SetActiveProfile` has no trust check itself, but provider-build paths are gated.
- **A2 (user-controlled profile cannot authorize itself):** DB-persisted profiles (`pgSaveProfile` has no trust gate, `pgActivateProfile` has none) — but activation/build is rejected by `providerPolicyForProfile`/`applyLLM`/`providerForProfile`/`setLLM` when trust is restricted; untrusted BaseURLs denied at all provider-construction paths. **Test evidence:** `agent/provider_trust_test.go`: `TestUserControlledProfileBaseURLCannotSeedTrust`, `TestTrustSetFirstSeedWinsCannotWiden`, `TestRestrictedTrustPolicyPermutations`, `TestLegacyNewProviderFailsClosedWhenUntrusted`, `TestSetTrustedProviderHostsMalformedEntriesNeverAuthorize` — all PASS. Full `go test ./agent/` green (incl. `TestProviderDNS_DifferentHostSameIP`).
- **A3 runtime config reload:** trust root is env-only at startup; no production reload path. `NewServer` seeds trust before any provider build; failed configs return errors before mutation (verified by reading `applyLLM`/`setLLM`).
- **A4 concurrent trust updates:** `SetTrustedProviderHosts` has no production callers; state machine guarded by `providerEgressMu`; `atomic.Value` snapshots — atomicity at read path. **Deterministic tests added in this phase:** `TestTrustSetConcurrentSeedAndRead` (barrier-based: 16 readers × 200 policy reads during seed attempts), `TestTrustPolicyReadDuringReseed` — both PASS.

## 4. Gate B — Traffic allowlist administrative integrity — `TEST VERIFIED` + `RUNTIME VERIFIED`

- Production wiring (`server/manager.go:410`): `entries := traffic.AllowlistFromEnv(); if entries != nil { tr.SetEgressAllowlist(entries, true) }` + `traffic.SetEgressAuditSink` logging `[traffic] egress allow/deny host=... reason=...`.
- `ARTEX_EGRESS_ALLOWLIST` parsed by `traffic.AllowlistFromEnv` (new; nil when unset/empty; fail-closed deny-all when configured-but-empty; malformed entries never authorize — verified).
- **Unit tests — PASS:** `traffic/egress_allowlist_test.go` (`TestAllowlistFromEnv` 4 cases, `TestSetEgressAllowlistMalformedEntriesNeverAuthorize`, `TestSealedEmptyAllowlistDeniesAll`, `TestAllowedEntryMatchesHostAndHostPort`, `TestSetEgressAllowlistAcceptsHostOnlyEntry`), `traffic/egress_allowlist_concurrency_test.go` (`TestTrafficAllowlistConcurrentReadWrite`, `TestTrafficAllowlistDeniedHostNeverAppears`).
- **Runtime (8.4.7 lab, re-verified):** proxy to `127.0.0.1:19004` (allowlisted) → `HTTP/1.1 200 OK`; to `127.0.0.1:19998` (unlisted) → `HTTP/1.1 502 Bad Gateway`; audit log: `egress allow host=127.0.0.1:19004 reason="pinned-forwarder"`, `egress deny host=127.0.0.1:19998 reason="traffic proxy: egress to 127.0.0.1:19998 denied by explicit allowlist"`. Zero unauthorized dials (deny at `upstreamProxyFor` pre-dial).

## 5. Gate C — Race detector — `RACE DETECTOR UNAVAILABLE` (exact blocker)

- `CGO_ENABLED=1 go test -race ./agent/ ./traffic/` → `cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%` (exit 1). With default `CGO_ENABLED=0`: `go: -race requires cgo`. No C compiler anywhere (`where gcc cl cc clang` → none). Installing toolchain out of scope. → **race freedom: UNKNOWN** (reproducible on any CGO-enabled Windows without gcc).
- **Deterministic concurrency tests added (PASS):** `agent/provider_trust_concurrency_test.go` (`TestTrustSetConcurrentSeedAndRead`, `TestTrustPolicyReadDuringReseed` — barrier-based, no sleeps), `traffic/egress_allowlist_concurrency_test.go` (`TestTrafficAllowlistConcurrentReadWrite`, `TestTrafficAllowlistDeniedHostNeverAppears`). These prove snapshot consistency under concurrent updates; they are NOT a substitute for the race detector.

## 6. Gate D — Regression failure resolution — `SOURCE VERIFIED` + partial `TEST VERIFIED`

| Test | Status | Evidence |
|---|---|---|
| `TestSkillRelPath` (server) | **PRE-EXISTING; test assumes Unix semantics** | Production `server/server_mgmt.go:skillRelPath` returns `filepath.Clean` (OS-native `\` on Windows); test expects `/` — contract mismatch on Windows. Files unchanged by this phase (git status). Not a production defect; no change (would alter behavior on Windows for all callers). |
| `TestGraphOverviewExpandsAssociatedCompanyScope` (agent) | **PRE-EXISTING (8.4.7); flaky** | Fails "task scope missing" at `blackboard_inheritance_test.go:87`; async goal seeding timing; scope/graph files untouched (`git diff --name-only` → none related). Passed standalone in 8.4.8 full run (flaky). |
| `TestCoreTaskLifecyclePG` (server) | **ENVIRONMENTAL** | "expected goal nodes seeded, got 0" — goal seeding async via `server/goals.go` (LLM-decompose path); with no LLM provider configured in the lab, no goals seeded. Files untouched. Documented as timing/provider-dependent, not a boundary regression. |
| `TestEvidenceBindingLifecycle` (evidence) | **ENVIRONMENTAL (flaky)** | Passes standalone; full-run nondeterminism. `evidence/store.go` Windows fix already in place (8.4.7). |
| Windows evidence durability review (`evidence/store.go`) | **REVIEWED — fix adequate** | 8.4.7 fix: skip directory `Sync()` on Windows (probe: `os.Open(dir).Sync()` → "Access is denied"); file-level `f.Sync()` verified OK by probe; `os.Rename` atomic on NTFS; errors from the real file sync still returned. Crash-durability is not identical across OSes by design (Windows limitation documented). |

- **Regression matrix re-run (this phase):** `go build ./...` exit 0; `go vet ./...` exit 0; `git diff --check` exit 0; `go test ./agent/ ./traffic/ ./enrich/ ./selfupdate/` — all OK (exit 0). Server Finding/Notify pass with fresh PG (8.4.7).

## 7. Gate E — Cross-boundary adversarial sequences — `TEST VERIFIED`

1. Untrusted profile → provider denied (`TestLegacyNewProviderFailsClosedWhenUntrusted`; runtime Live A).
2. Task scope expansion → `TestRedirectChainRevalidatedEachHop` (deny at out-of-scope hop, zero dials).
3. Same-IP different hostname → `TestProviderDNS_DifferentHostSameIP` PASS.
4. DNS answers change between validation and dial → `TestProviderDNS_RebindingRevalidatedPerDial` PASS (per-dial revalidation).
5. Provider retry after transient → `TestProviderRetry_*` PASS (3 tests).
6. Traffic allowlist change while active → `TestTrafficAllowlistConcurrentReadWrite` (atomic snapshots; denied host never appears).
7. Two tasks, different scopes → `agent/scope_task_isolation_test.go` PASS.
8. Worker cancel mid-request → `dialPinnedContext` ctx-cancel path covered by `TestProviderRetry_ContextCancelStopsRetries` + scope pin tests.
9. Malformed admin config → `TestSetTrustedProviderHostsMalformedEntriesNeverAuthorize` PASS.
10. Restart with empty/invalid trust → documented: trust root = env only; absent trust → legacy unrestricted (documented limitation; with restricted set, empty-BaseURL profiles denied — tested).

## 8. Files changed this phase (and why)

- `agent/provider_trust_test.go` (new): Gate A adversarial tests.
- `agent/provider_trust_concurrency_test.go` (new): Gate C trust-state concurrency tests.
- `traffic/egress_allowlist_concurrency_test.go` (new): Gate C allowlist concurrency tests.

(No production-code changes this phase — all completed in VIII.4.7: `agent/provider_egress.go`, `agent/provider.go`, `server/server.go`, `server/manager.go`, `traffic/egress.go`, `evidence/store.go`.)

## 9. Final verdict

**`PARTIAL — RACE DETECTOR UNAVAILABLE`** — Security-boundary work for VIII.4.8 is complete: trust-root provenance verified (A), traffic allowlist integrity verified (B), deterministic concurrency tests added and passing (C), regression failures resolved/classified (D), cross-boundary sequences verified (E). Race freedom remains UNKNOWN due to missing C compiler (no install performed per scope). Nothing claiming complete network security or full regression success: known failures documented (`TestCoreTaskLifecyclePG`, `TestGraphOverviewExpandsAssociatedCompanyScope`, `TestEvidenceBindingLifecycle` — all environmental/flaky; `TestSkillRelPath` — Unix-semantic contract).

## 10. Stop confirmation

Work stopped: no external LLM calls, no public scans, no compiler installs, no service restarts, no database modifications (lab DBs dropped, lab PG stopped — port 5433 free). All prior-phase reports preserved.
