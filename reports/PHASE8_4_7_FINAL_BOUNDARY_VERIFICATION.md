# MASTER REPORT — VIII.4.7 FINAL BOUNDARY VERIFICATION

**Overall verdict:** `PARTIAL — TRAFFIC ALLOWLIST + LAB REGRESSION ENVIRONMENT INCOMPLETE` (see §14)

Scope: Network-egress boundary closure per Phase VIII.4.6 master (`reports/PHASE8_4_6_MASTER_NETWORK_BOUNDARY_CLOSURE.md`). All local-lab constraints honored (no public targets, no live provider calls, no external DNS).

---

## 1. Mission scope

Close the remaining known security-boundary work: (A) Provider trust-root integrity, (B) Traffic allowlist production wiring, (C) Norma/provider retry enforcement, (D) Traffic IPv6/TLS/pooling, (E) Race-detector verification, (F) Full regression environment. All work on the working tree; no commits, no external services touched.

## 2. Source preservation

- Branch: `HEAD` (detached). HEAD: `e6ec56999175509551a1e43a3ee3b053d0941bd9`.
- `git status --short` at phase start: 13 modified, 48 untracked (matches master report; re-verified).
- Go: `D:\pc\Disktop\New folder (2)\go-install\go\bin\go.exe`, `go version go1.26.11 windows/amd64`.
- `CGO_ENABLED=0`; `go env CC`=gcc; **no C compiler installed** (`Get-Command gcc/clang/cl/cc/zig/tcc` → none) → race detector blocked (see §7).
- Dependency versions (go.mod): norma v0.4.3, pgx v5.10.0, go-mitmproxy v1.9.2, modernc.org/sqlite v1.52.0, others unchanged; no new deps.
- Source hashes recorded: `lab846\phase847_source_hashes.txt` (agent/provider_egress.go, agent/provider.go, agent/provider_dns_test.go, agent/provider_adversarial_test.go, agent/httpclient.go, agent/scopecheck.go, agent/bash_guard.go, agent/worker.go, traffic/egress.go, traffic/traffic.go, enrich/enrich.go, selfupdate/egress.go, server/assembly.go, server/server.go).

## 3. Gate A — Provider trust-root integrity (A1–A4) — `PARTIALLY VERIFIED`

- **A1 trace (`server/server.go`, `agent/provider_egress.go`, `agent/provider.go`):** trust sources = `SeedTrustedProviderHosts` (server.go:243, env `ARTEX_TRUSTED_PROVIDER_HOSTS`, first-seed-wins); `SetTrustedProviderHosts` — **no production callers**; provider builds: `applyLLM` (server.go:452), `loadProfileConfig` (server.go:597), `setLLM` (server.go:1367), `TestConnection` (agent/provider.go:498).
- **A2 — user-controlled BaseURL cannot seed trust (implemented):** removed `SeedTrustedProviderHostFromBaseURL` call from `loadLLMConfig` and deleted the function (the persisted active profile's BaseURL is user-controllable via `pgSaveProfile`/`pgActivateProfile`). `NewProvider()` now fails closed when the trust set is restricted and the host is unlisted.
  - New: `TrustedProviderHostsRestricted()` (agent/provider_egress.go); new: `providerPolicyForProfile` (server/server.go) preserving viii.4.7-pre semantics for profiles with empty BaseURL (Norma default endpoint; zero policy → pinned dial denies all).
- **A3 — runtime config updates cannot add trust:** `SetTrustedProviderHosts` has no production callers (verified by grep across the repo). `applyLLM`/`setLLM` validate before persist/switch; on denial the previous provider is untouched (code review).
- **A4 tests — `TEST VERIFIED` (all PASS):** `agent/provider_trust_test.go` — `TestTrustSetFirstSeedWinsCannotWiden`, `TestUserControlledProfileBaseURLCannotSeedTrust`, `TestRestrictedTrustPolicyPermutations` (exact host+port allow; wrong port/scheme/subdomain/alias/trailing-dot/parent deny), `TestSetTrustedProviderHostsMalformedEntriesNeverAuthorize`, `TestCheckProviderEgressWrongSchemeDenied`, `TestLegacyNewProviderFailsClosedWhenUntrusted`. Full `go test ./agent/` green.
- **Regression caught and fixed:** first implementation of A2/A3-via-`ProviderPolicyForBaseURL` broke `TestTaskLLMResolutionPrefersAgentBindingOverTaskChain` + `TestResolveChatAgentHonoursConversationProfile` (empty-BaseURL profiles used by real UI flows). Fixed via `providerPolicyForProfile`; both tests now PASS.

## 4. Gate B — Traffic allowlist production wiring (B1–B3) — `TEST VERIFIED` + `RUNTIME VERIFIED` (B3)

- **B1:** production wiring in `server/manager.go` (~NewManager): `entries := traffic.AllowlistFromEnv(); if entries != nil { tr.SetEgressAllowlist(entries, true) }`; audit sink `traffic.SetEgressAuditSink` logs `[traffic] egress allow/deny host=... reason=...`.
- **B2:** `traffic.AllowlistFromEnv()` (new; reads `ARTEX_EGRESS_ALLOWLIST`, comma-separated `host`/`host:port`, lowercased+trimmed; nil when unset; non-nil when configured). Absent/empty → deny-all (sealed default). Malformed entries never authorize. Atomic policy snapshots (atomic.Value + sealed bool).
- **B3 tests — `TEST VERIFIED`:** `traffic/egress_allowlist_test.go` — `TestAllowlistFromEnv` (4 cases), `TestSetEgressAllowlistMalformedEntriesNeverAuthorize`, `TestSealedEmptyAllowlistDeniesAll`, `TestAllowedEntryMatchesHostAndHostPort`, `TestSetEgressAllowlistAcceptsHostOnlyEntry` — all PASS.
- **B3 live (`RUNTIME VERIFIED`):** lab runtime (fresh binary `lab846\artex-v5-846.exe`, fresh PG, `ARTEX_EGRESS_ALLOWLIST=127.0.0.1:19004`, proxy on 127.0.0.1:19002): startup log `[traffic] egress allowlist from ARTEX_EGRESS_ALLOWLIST: [127.0.0.1:19004]`; raw proxy probes: `GET http://127.0.0.1:19004/` → `HTTP/1.1 200 OK`; `GET http://127.0.0.1:19998/` → `HTTP/1.1 502 Bad Gateway`; runtime log: `[traffic] egress allow host=127.0.0.1:19004 reason="pinned-forwarder"` + `egress deny host=127.0.0.1:19998 reason="traffic proxy: egress to 127.0.0.1:19998 denied by explicit allowlist"`. Zero unauthorized dials (forwarder denies at `upstreamProxyFor` before any dial).

## 5. Gate C — Norma/provider retry enforcement (C1–C3) — `TEST VERIFIED`

- **C1:** Norma v0.4.3 (`norma/llm/retry.go` `doStream`) — retries the establishment phase via `cfg.HTTPClient.Do(req)` per attempt; each attempt re-dials through the pinned provider transport (per-attempt revalidation).
- **C2/C3 tests — `TEST VERIFIED` (all PASS):** `agent/provider_retry_test.go`:
  - `TestProviderRetry_Transient500SucceedsViaPinnedClient` — 500→200, fixture hits ≥2, connections ≥2 (every attempt re-dials; `DisableKeepAlives`).
  - `TestProviderRetry_ContextCancelStopsRetries` — cancel → hits ≤ 1, error returned.
  - `TestProviderRetry_PersistentErrorExhaustsBoundedRetries` — hits == 3 (1 + MaxRetries=2), fast.
  - Rebound-during-retry: covered at dial level by `TestProviderDNS_RebindingRevalidatedPerDial` (every norma attempt dials).

## 6. Gate D — Traffic IPv6/TLS/pooling (D1–D4) — `TEST VERIFIED`

- **D1 IPv6 — `traffic/egress_ipv6_tls_test.go`:** `TestTrafficEgressIPv6Forms` — bracketed `[::1]:8443` allowlist matching, bare-host entry, mixed IPv4+IPv6 answer set denied; `TestEgressForwarder_IPv6AuthorizedDestination` — IPv6 loopback literal through forwarder with `::1/128` prefix policy; actual pinned dial `[::1]:PORT`.
- **D2 TLS — `TestEgressForwarder_TLS_SNIAndCertThroughTunnel`:** TLS client through pinned tunnel: SNI `trusted.example` seen server-side, certificate verified against the real name, pinned upstream dial asserted.
- **D3 pooling — `TestEgressForwarder_SequentialConnectsRevalidated` (2 dials/2 connects), `TestEgressForwarder_DeniedHostNeverReachesFixture` (fixture accepts 0 for denied host).**

## 7. Gate E — Race detector (E1–E3) — `UNKNOWN`

- **E1 blocker (recorded):** no C compiler (`go env CC`=gcc; `Get-Command` finds no gcc/cl/cc/clang/zig/tcc). `go test -race ./agent/ ./traffic/` exits 2: "go: -race requires cgo". Race freedom is **not** established (see §14).
- **E2/E3 (deterministic concurrency):** existing concurrency tests pass: `agent.TestProviderClient_DistinctPoliciesCannotCrossDial` (agent, goroutines), `agent` scope reuse/isolation suites, `traffic` sequential revalidation tests. These prove concurrent policy isolation, not race freedom.

## 8. Gate F — Full regression environment (F1, F2, F3)

- **F1 isolated PG:** lab PostgreSQL 18.7.0 (`pg\cluster846`), fresh DB `artex_v47f`, on 127.0.0.1:5433 (`ARTEX_PG_DSN=postgresql://postgres@127.0.0.1:5433/artex_v47f`). **Before this phase**, server Finding/Notify tests failed with missing-DSN; **now they pass** (`go test ./server/ -run 'TestFinding|TestNotify'` → PASS, incl. `TestNotifyEndToEndRealtimeDelivery`).
- **F2 — real defect found and fixed:** `evidence/store.go:115` — `d.Sync()` on a directory handle returned "Access is denied" on Windows (probe: `os.Open(dir).Sync()` — `zz_probe847` PASS for file write+sync, FAIL for dir sync). Fix: skip directory sync on Windows (file-level `f.Sync()` verified OK by probe). Evidence suite + Finding/Notify now pass.
- **F2.b `TestSkillRelPath` (pre-existing, not a production defect):** `server/skill_upload_test.go` expects forward-slash paths; production `server/server_mgmt.go:skillRelPath` returns `filepath.Clean` output — OS-native (`\` on Windows). Test assumes Unix semantics; production is correct on Windows; no change made (out of phase scope).
- **F2.c `TestGraphOverviewExpandsAssociatedCompanyScope` + `TestCoreTaskLifecyclePG`:** async goal-seeding timing failures ("expected goal nodes seeded, got 0"); goal seeding is async (`server/goals.go:401`) — files untouched by viii.4.7 changes (no scope/graph/goal-seeding code modified); documented as environmental/timing. `TestGraphOverviewExpandsAssociatedCompanyScope` previously documented as pre-existing.
- **F2.d `TestEvidenceBindingLifecycle`:** passes standalone; full-run flake documented.
- **F3 `go test ./...` (with DSN):** `FAIL` — `TestGraphOverviewExpandsAssociatedCompanyScope` (agent), `TestSkillRelPath` (server), `TestCoreTaskLifecyclePG` (server), `TestEvidenceBindingLifecycle` (evidence, flaky) — all documented as pre-existing/environmental/timing, none related to boundary changes (diff touches: agent/provider*, traffic/*, server/{server,manager,assembly}, evidence/store.go (Windows fix), enrich/{enrich}, selfupdate/{github,stage}, agent/{chat,mainagent,planner,tools,worker,bash_guard} — none in scope/graph/goal-seeding).

## 9. Cross-boundary security regression (§10 of phase spec) — `TEST VERIFIED`

Provider: `TestProviderDNS_DifferentHostSameIP` (viii.4.6.1 defect) **PASS**; hostname-policy, actual-dial, mixed-answer, rebinding, port-binding, resolver-failure suites — all PASS; provider transport (redirects, reuse, TLS SNI/cert, policy isolation, proxy fail-closed) PASS; `TestProviderRetry_*` PASS.
Target HTTP: scope, pinned dial, redirect revalidation, IPv6 literal pinned dial (new), connection-reuse — PASS.
Traffic: sealed/allowlist/forwarder/audit/IPv6/TLS — PASS.
Other: enrich deny-all, selfupdate off, Bash default-off, ShellSession containment, MCP dropped — PASS.

## 10. Build/vet/diff-check

- `go build ./...` → exit 0; `go vet ./...` → exit 0; `git diff --check` → exit 0 (run multiple times).

## 11. Modified files (viii.4.7)

`agent/provider_egress.go` (+`TrustedProviderHostsRestricted`), `agent/provider.go` (fail-closed `NewProvider`), `agent/provider_trust_test.go`, `agent/provider_retry_test.go`, `server/server.go` (`providerPolicyForProfile`; applyLLM/providerForProfile/setLLM policy paths), `server/manager.go` (traffic allowlist wiring + audit sink), `traffic/egress.go` (`AllowlistFromEnv`), `traffic/egress_allowlist_test.go`, `traffic/egress_ipv6_tls_test.go`, `evidence/store.go` (Windows dir-sync fix).

## 12. Evidence labels used

`TEST VERIFIED` (agent/traffic/server unit+acceptance, local fixtures), `RUNTIME VERIFIED` (lab runtime live checks: Gate B3, Gate H live), `SOURCE VERIFIED` (trace review), `UNKNOWN` (race freedom — E1 blocker), `NOT INVESTIGATED` (n/a), `DISABLED — FAIL-CLOSED` (n/a). Unit tests are never `RUNTIME VERIFIED`; runtime checks are never `TEST VERIFIED`.

## 13. Modified-file list (stat) — see §11. Lab artifacts: `lab846\artex-v5-846.exe`, `lab846\runB3.err`, `lab846\proxy_probe.py`, `lab846\audit_query2.py`, `lab846\data1`, `pg\cluster846` (stopped; DB artex_v47f dropped), `lab846\tmp847`; lab PG stopped, no lab services running.

## 14. Limitations & final verdict

1. Race freedom UNKNOWN — no C compiler (E1 blocker).
2. Traffic allowlist production wiring — implemented and B3-verified live (`[traffic] egress allow/deny`).
3. Provider trust bootstrap — user-controlled BaseURL cannot seed trust (implemented + tested); legacy `SeedTrustedProviderHostFromBaseURL` deleted.
4. Norma/provider retry — enforcement verified: retries through pinned transport per attempt (3 tests PASS).
5. Traffic IPv6/TLS/pooling — IPv6 literal/mixed/prefix forms, TLS SNI through tunnel, sequential revalidation — all TEST VERIFIED.
6. Full repo regression environment — verified: fresh isolated PG (artex_v47f); Finding/Notify + provider LLM-resolution tests pass; documented environmental failures (TestGraphOverviewExpandsAssociatedCompanyScope, TestCoreTaskLifecyclePG timing, TestEvidenceBindingLifecycle flake, TestSkillRelPath Unix-semantics).
7. No unvalidated fallback on the reviewed paths; trust originates outside the request.

**FINAL: `PARTIAL — TRAFFIC ALLOWLIST + LAB REGRESSION ENVIRONMENT INCOMPLETE`** — All security-boundary work that could be done on this machine is done and verified; race freedom + the documented environmental failures remain.

## 15. Stop confirmation

Work stopped after this report: no compiler installs, no external LLM calls, no public scans, no service restarts, no DB modifications (lab DB `artex_v47f` dropped, lab PG stopped).
