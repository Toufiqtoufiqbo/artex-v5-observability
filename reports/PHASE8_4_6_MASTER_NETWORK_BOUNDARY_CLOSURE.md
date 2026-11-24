# MASTER REPORT â€” VIII.4.6 NETWORK BOUNDARY CLOSURE (PHASE VIII.4.6, MASTER)

**Phase:** VIII.4.6 MASTER (network-egress gap closure; Gates Aâ€“H)
**Artifacts:** fresh binary `lab846\artex-v5-846.exe` (SHA256 `0C71DFB19AB3F7C10F9262BDB7D4BAB8BA70A07086E371AE9977854B56BD681E`), fresh lab DB `artex_v46h` (PostgreSQL 18.7.0 on 127.0.0.1:5433, cluster `pg\cluster846`)
**Overall verdict:** `PASS â€” FAIL-CLOSED GAPS CLOSED (TEST+RUNTIME VERIFIED)` with documented limitations

---

## 1. Summary of confirmed defect and its resolution (VIII.4.6.1 â†’ Gate A)

- Eighth assessment (VIII.4.6.1, `reports/PHASE8_4_6_1_PROVIDER_ADVERSARIAL_VERIFICATION.md`, verdict `FAILED â€” UNTRUSTED PROVIDER DESTINATION CAN BE DIALED`): `providerPinnedDialWithResolver` authorized a request hostname **only by comparing its resolved IP set against the trusted host's IP set**, so `evil.example` (any hostname) resolving to the same IP as `trusted.example` passed and was dialed. The failing test was `TestProviderDNS_DifferentHostSameIP`.
- Gate A fix (production, in `agent/provider_egress.go` + `agent/provider.go` + `server/server.go`):
  - Exact hostname identity check first: the request host must equal the policy host (`canonicalProviderHost`: lowercase/trim; trailing dot NOT stripped; `validProviderHost`: ASCII letters/digits/`-`,` .`,`:`,`_` only), plus independent port and scheme, or be explicitly listed in the trusted host set (`ProviderPolicyForBaseURL`, `hostTrustedLocked`). A global trusted policy (`SetTrustedProviderPolicy`) is honored only for the same host.
  - `providerPinnedDialWithResolver` rewritten: hostname must be authorized **before** any IP comparison; then single DNS resolution, mixed-answer-set rejection (both orders), nil/malformed IP rejection, IP-literal only if explicitly named by policy (`providerEgressPolicyFor` for literal destinations: `addressInProviderPrefixes`), per-dial revalidation of rebinding.
  - `agent/provider.go`: `quotaAwareHTTPClientWithPolicy` + `Config.NewProviderWithPolicy` (per-profile policy isolation, no shared global policy); proxy mode fails closed at construction (`incompatible with pinned provider egress`); `DisableKeepAlives=true`, `ForceAttemptHTTP2=false`, `TLSNextProto={}`; redirects rejected (`CheckRedirect` error).
  - Runtime wiring (`server/server.go`): `agent.SeedTrustedProviderHosts(agent.TrustedProviderHostsFromEnv()...)` at startup (env `ARTEX_TRUSTED_PROVIDER_HOSTS` comma-separated `host`/`host:port`; falls back to `ARTEX_LLM_BASE_URL` host:port); `agent.SeedTrustedProviderHostFromBaseURL` at startup persisted-profile load; `applyLLM` + `setLLM` + engine provider build (`loadProfileConfig` path, line ~591) deny untrusted BaseURL; `TestConnection` uses `ProviderPolicyForBaseURL`.
  - New operator APIs: `SetTrustedProviderHosts` (replace), `SeedTrustedProviderHosts` (first-seed-wins), `TrustedProviderHostsFromEnv`, `SeedTrustedProviderHostFromBaseURL`, `ProviderPolicyForBaseURL`, `CheckProviderBaseURLTrusted`; startup log `[trusted] provider hosts=[...]`.
- Verification of the fix on the current tree (TEST VERIFIED):
  - `go test ./agent/ -run 'TestProviderDNS_DifferentHostSameIP|TestProviderDNS_TrustedAndMixedAnswers|TestProviderDNS_ResolverFailures|TestProviderAdversarial' -count=1` â†’ **PASS, exit 0** (0/4 earlier failure).
  - `TestProviderDNS_DifferentHostSameIP` (the VIII.4.6.1 failing test) â€” **PASS** (previously `--- FAIL: fake dial [1.2.3.4:443]`).
  - `TestProviderAdversarial_HostnamePolicy` â€” PASS (subdomain/parent/suffix-lookalike/trailing-dot/wrong-scheme/wrong-port DENIED).
  - `TestProviderAdversarial_ActualDialDestination` â€” PASS (`httptrace.GotConn` confirms RemoteAddr on the authorized host).
  - Full `go test ./agent/ -count=1` â€” PASS.

---

## 2. Scope and constraints (all honored)

- Local lab only: no public targets, no external scans, no live NVIDIA/provider calls, no external DNS, no live inference.
- No secrets printed or committed (report contains no keys, tokens, cookies, or Authorization headers; the JWT used in runtime probes is not recorded).
- Baseline archive `artex-global-main.zip`, Planner/Worker prompts, detection logic, dependency versions unchanged. No dependencies added or removed; `git diff --check` clean. No destructive git ops; nothing pushed.
- No weakening of TLS verification, scope controls, or security test assertions; no removal of failing tests.
- No claiming of complete network security; provider/traffic pooling, TLS SNI runtime proof, IPv6 runtime proof, and race freedom are documented as UNKNOWN below.

---

## 3. Evidence labels used

| Label | Meaning |
|---|---|
| `SOURCE VERIFIED` | Static code review of the boundary |
| `TEST VERIFIED` | Deterministic local unit/acceptance tests (never claimed for unit tests) |
| `RUNTIME VERIFIED` | Live runtime checks against the isolated binary |
| `PARTIALLY VERIFIED` | Covers the boundary; documented limitations remain |
| `UNKNOWN` | Not investigated / not provable in this environment |
| `NOT INVESTIGATED` | Out of phase scope |
| `DISABLED â€” FAIL-CLOSED` | Uncontainable paths explicitly disabled with this label |

---

## 4. Changes made (VIII.4.6 Gates Aâ€“H)

- Gate A (viii.4.6.1 defect): `agent/provider_egress.go` (+ `urlPortOf`, `urlSchemeOf` helpers), `agent/provider.go` (`NewProviderWithPolicy`, `quotaAwareHTTPClientWithPolicy`), `server/server.go` (startup seeding + runtime gates).
- Gate B: `agent/provider_transport_test.go` (new, 5 tests); `agent/provider.go` refactor above; runtime probes (Gate H).
- Gate C: `agent/target_boundary_test.go` (new, 3 tests).
- Gate D: `traffic/egress_dial.go` (new: `egressDialer`, `egressForwarder` loopback CONNECT forwarder, `trafficPrefixAllowed`), `traffic/egress.go` (`EgressAllowedAddr`, `SetEgressAddressPolicy`, `upstreamProxyFor` hook, audit calls), `traffic/traffic.go` (forwarder wiring + `Close`), `traffic/egress_dial_test.go` (new, 8 tests).
- Gate E: `agent/bash_guard.go` (`LocalShellAllowed`, `ContainLocalShell`), `agent/worker.go`, `agent/planner.go`, `agent/mainagent.go`, `agent/chat.go` (containment on assembled surfaces), `agent/bash_guard_test.go` (+2), `server/assembly.go` (ShellSessionTools gating), `agent/enrich` + `agent/selfupdate` gating and tests, `agent/chat.go`, `agent/planner.go`, `agent/mainagent.go` (WebFetch/WebSearch proxy-dependent).
- Gate F/G/H: no new production logic; verification + report.
- All work committed to the working tree only (uncommitted, as required).

---

## 5. Gate A â€” provider dial boundary (hostname identity + per-dial pin)

- Policy model: `ProviderEgressPolicy{ExpectedHost, ExpectedPort, ExpectedScheme, AllowedPrefixes, ExtraValidators}`; three independent egress domains (`EgressTargetAssessment`, `EgressLLMProvider`, `EgressInternalService`) â€” none authorizes another.
- `providerEgressPolicyFor(baseURL)`: fail-closed on missing/empty/malformed host or non-ASCII (`validProviderHost`); canonical host (lowercase/trim; trailing dot NOT stripped); when a trusted host set is installed (`providerHostsSeeded` + `providerTrustedHosts`), the BaseURL host must match exactly (`hostTrustedLocked`, supporting both `host` and `host:port` entries); global `trustedProviderPolicyLoaded()` honored only for the same host.
- `providerPinnedDialWithResolver`: request-host == policy host (case-insensitive) else deny; port must match policy port; scheme must match policy scheme; single DNS resolution; all answers authorized (`selectAuthorizedIP`-style: mixed set rejected, both orders); nil IP rejected; IP-literal destinations only if explicitly named by policy; per-dial revalidation (`resolvePinnedIP` each dial â€” rebinding test passes).
- `Test verdicts`: `go test ./agent/` PASS (4.4s); adversarial suite PASS (see Â§1).

---

## 6. Gate B â€” provider transport closure

`agent/provider_transport_test.go` (new, 5 tests) â€” TEST VERIFIED, all PASS:

1. `TestProviderClient_RejectsRedirects` â€” redirect target hits = 0; error contains `redirects are not allowed`.
2. `TestProviderClient_NoConnectionReuse` â€” 2 requests â†’ exactly 2 new connections (`DisableKeepAlives`).
3. `TestProviderTLS_SNIAndCertificateVerification` â€” SNI == `trusted.example` survives pinning; hostname-mismatched cert fails verification.
4. `TestProviderClient_DistinctPoliciesCannotCrossDial` â€” two providers (A/B) each reach only their own endpoint; cross-policy dials denied.
5. `TestProviderClient_ProxyModeFailsClosed` â€” provider proxy mode fails at construction: `incompatible with pinned provider egress`.

Additional: keepalive/HTTP2 off; redirects rejected per request.

---

## 7. Gate C â€” target HTTP security boundary

`agent/target_boundary_test.go` (new, 3 tests) â€” TEST VERIFIED, all PASS:

1. `TestRedirectChainRevalidatedEachHop` â€” chain `/a â†’ /b â†’ http://10.255.255.1:1/out` denied at the out-of-scope hop; dial-seam recorded **no dial to `10.255.255.1`** (only the in-scope hops).
2. `TestDoHTTP_IPv6LiteralPinnedDial` â€” loopback `[::1]:port` scope `::1/128`; actual dial address recorded by the dial seam == bracketed `[::1]:port` (IPv6 literal dial pinned).
3. `TestDoHTTP_IPv6LiteralUnauthorizedZeroDials` â€” unauthorized IPv6 literal denied with zero dials.

Existing scope tests re-run: `agent` suite PASS; `scope_boundary_test.go` (4 tests: mixed reject, empty/all-authorized, root_domain not authorizing special addresses, bracketed IPv6 `hostOf`) â€” PASS. Redirect revalidation: `TestRedirectScopeRevalidated` (VIII.2.1) â€” PASS.

---

## 8. Gate D â€” traffic proxy upstream destination enforcement (VIII.4.6)

Implementation (`traffic/egress_dial.go`, `traffic/egress.go`, `traffic/traffic.go`):

- go-mitmproxy v1.9.2 only exposes `SetUpstreamProxy`; the hook is the pre-dial gate. `Traffic.upstreamProxyFor` checks `EgressAllowed` (explicit allowlist) â†’ when sealed, rejects chained upstream proxies â†’ returns loopback URL of the **pinned forwarder**.
- `egressForwarder`: loopback-only CONNECT forwarder (`127.0.0.1:0`). For each CONNECT it runs `egressDialer.dialPinned`: allowlist check (`EgressAllowedAddr`: host / host:port entries), address-prefix check (`SetEgressAddressPolicy`), literal authorization, single DNS resolution, mixed-answer-set rejection (both orders), zero dials on any denial. Successful tunnel dials the exact authorized IP. Original hostname preserved in the CONNECT authority â†’ client TLS SNI/cert untouched (by design of the proxy).
- Audit: `emitTrafficEgressAudit(host, allowed, reason)` on every decision (sink wired via `traffic.SetEgressAuditSink`; sink verified by `TestTrafficProxyAuditSink` â€” production wiring of both audit and allowlist noted as follow-up, see Â§14).

Acceptance tests (`traffic/egress_dial_test.go`, new, 8 tests) â€” TEST VERIFIED, all PASS:
1. `TestEgressForwarder_AuthorizedDestinationPinned` â€” authorized CONNECT â†’ 200 â†’ tunnel echoes (proves actual dial to the pinned fixture).
2. `TestEgressForwarder_UnlistedHostAndPortDeniedZeroDials` â€” unlisted host/port â†’ 502, dials=0.
3. `TestEgressForwarder_MixedAnswersDenied` â€” mixed answers both orders â†’ 502, dials=0.
4. `TestEgressForwarder_ResolverFailuresZeroDials` â€” error/empty/malformed/cancel â†’ 502, dials=0.
5. `TestEgressForwarder_RebindingRevalidatedPerConnection` â€” second connection after DNS rebind â†’ 502, dialer called once.
6. `TestEgressForwarder_ListenerIsLoopbackOnly` â€” bound to 127.0.0.1.
7. `TestTraffic_SealedPolicyAndChainedProxy` â€” sealed allowlisted destination uses pinned forwarder; unlisted denied; chained upstream proxy â†’ fail closed.
8. (plus `traffic/egress_test.go` re-run â€” PASS.)

Finding (documenting fail-closed state): `SetEgressAllowlist` (and `SetEgressAddressPolicy`, `SetEgressAuditSink`) have **no production caller** â€” the proxy starts sealed with an empty allowlist, so sealed-empty denies everything (fail-closed default). Live checks confirm denial (Â§11, Live C/D).

---

## 9. Gate E â€” local shell and Web tool containment

- `agent/bash_guard.go`: `LocalShellAllowed()` â€” operator opt-in via `ARTEX_ALLOW_LOCAL_SHELL` (`1|true|yes|on`); **Bash removed from every agent surface by default** (Planner, Worker, MainAgent, Chat). `ContainLocalShell` filter applied after tool augmentation (`agent/planner.go`, `agent/worker.go`, `agent/mainagent.go`, `agent/chat.go`); with opt-in, Bash is wrapped by the network-command guard. `dropMCPTools` + `ContainLocalShell` both applied at worker surface (`agent/worker.go`).
- Web tools: `EnableWebFetch`/`EnableWebSearch` are now **true only when a sealed traffic proxy is configured** (`strings.TrimSpace(proxyAddr) != ""`); `WebSearch.Proxy` independent.
- `server/assembly.go`: `ShellSessionTools` injection gated by `agent.LocalShellAllowed() && !actool.InteractiveShellDisabled()`.
- Tests: `TestContainLocalShell_DefaultRemovesBashAndMCP` PASS; `TestContainLocalShell_OptInWrapsBash` PASS; `TestBashGuard_RejectsNetworkCommands`/`AllowsLocalCommands` PASS; `enrich` default-deny test (`TestEnrichDefaultDenyDropsJobsBeforeAnyNetwork`) PASS; `enrich` + `selfupdate` suites PASS.

---

## 10. Gates F & G â€” lifecycle review + adversarial + race attempt

- F (shared authorization/connection lifecycle): single transport choke point for target traffic (`doHTTP` â†’ `scopeGuard` â†’ `CheckScopeForKey` â†’ `dialPinnedContext`), single choke point for provider traffic (`providerPinnedDialWithResolver` inside `quotaAwareHTTPClientWithPolicy`), single choke point for traffic proxy upstream (`upstreamProxyFor`). No alternate dial paths found. Redirect revalidation covers every hop (`CheckRedirect` + `scopeGuard`).
- G (adversarial + race): adversarial/DNS suites PASS (see Â§1); `go test -race ./agent/ ./traffic/ -count=1` â†’ **exit=2: `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`** â†’ **race freedom UNKNOWN** (no gcc).

---

## 11. Gate H â€” isolated runtime validation (RUNTIME VERIFIED)

Environment: fresh lab PG **PostgreSQL 18.7.0** on `127.0.0.1:5433` (new cluster `pg\cluster846`, no pre-existing data), fresh DB **`artex_v46h`** (created by the runtime via `ensureDatabase`), fresh binary **`lab846\artex-v5-846.exe`** (SHA256 recorded), isolated ports (backend `127.0.0.1:19001`, proxy `127.0.0.1:19002`, fixture `127.0.0.1:19004`, lab PG `127.0.0.1:5433`). Python fixture `python -m http.server 19004` served as the "listed destination" fixture. All runtime commands used the runtime's own HTTP API (`/api/health`, `/api/auth/login`, `/api/llm`); no external targets contacted.

**Stage 1** â€” runtime with `ARTEX_TRUSTED_PROVIDER_HOSTS=127.0.0.1:19999` (unlisted vs. live checks):
- `POST /api/llm` `{provider:openai, base_url:http://127.0.0.1:19004/v1}` â†’ deny: `{"error":"provider egress: base_url host is not authorized by trusted provider configuration"}` â€” `LIVE_A_OK`.
- `POST /api/llm` `{base_url:http://127.0.0.1:19998/v1}` (listed host, different port) â†’ deny â€” `LIVE_A2_OK`.
- `POST /api/llm` `{base_url:http://localhost:19999/v1}` (alias form of trusted host) â†’ deny â€” `LIVE_A3_OK`.

**Stage 2** â€” runtime restarted with `ARTEX_TRUSTED_PROVIDER_HOSTS=127.0.0.1:19004`:
- Startup log: `[trusted] provider hosts=[127.0.0.1:19004]`.
- `POST /api/llm` `{base_url:http://127.0.0.1:19004/v1}` â†’ **200** `{"configured":true, "provider":"openai", "model":"gB-115058", ...}` â€” `LIVE_B_OK`.
- `POST /api/llm` `{base_url:http://localhost:19999/v1}` â†’ deny â€” `LIVE_B2_OK` (still restricted).
- Live C â€” raw proxy probe (`GET http://127.0.0.1:19998/ HTTP/1.1` sent to the proxy at `127.0.0.1:19002`): `HTTP/1.1 502 Bad Gateway` â€” **no 2xx/3xx reach; dials to 19998 = 0 by construction (deny at `upstreamProxyFor` â†’ `writeForwarderStatus(502)`)**. `LIVE_C_OK`.
- Live D â€” raw proxy probe to the listed destination `http://127.0.0.1:19004/`: also `HTTP/1.1 502 Bad Gateway` â€” sealed-empty allowlist fails closed (no production `SetEgressAllowlist` caller; the pinned-forwarder reach path with a populated allowlist is covered by unit tests, Â§8). Direct GET to 19004 (bypassing the proxy) â†’ `200` (python fixture logged `GET / HTTP/1.1 200` at 11:47:23, 11:48:51, 11:49:07, 11:51:32).
- DB evidence: `artex_v46h` exists; `llm_profiles` rows present (`gH-A`, `gH-A-114158`, `gH-A3-...`, `default` with `base_url=http://127.0.0.1:19004/v1`).
- Note on PowerShell: `Invoke-WebRequest -Proxy` for loopback/localhost targets is bypassed by .NET's localhost proxy bypass â€” observed `200` there is the direct fetch, not the proxy.

**Stage 3 â€” cleanup:** runtime stopped (exit -1 = SIGTERM), python fixture stopped, lab PG stopped. All ports (19001/19002/19004/5433) free.

---

## 12. Test matrix results (final)

| Package | Result | Notes |
|---|---|---|
| `go build ./...` | exit 0 | â€” |
| `go vet ./...` | exit 0 | â€” |
| `git diff --check` | exit 0 | â€” |
| `go test ./agent/` | PASS | includes: provider DNS/adversarial (VIII.4.6.1 fixed), target boundary, scope pin/reuse, scope fail-closed, provider transport (Gates B), bash guard, containment |
| `go test ./traffic/` | PASS | sealed proxy, allowlist, audit sink, forwarder acceptance (Gate D) |
| `go test ./enrich/` | PASS | deny-all default, TLS verification |
| `go test ./selfupdate/` | PASS | egress default off |
| `go test ./...` (all) | 23 FAIL â€” all pre-existing/environmental (verified unmodified files) | 22 Ã— `TestFinding*`/`TestNotify*` fail: `æœªæ‰¾åˆ°æ•°æ®åº“é…ç½®` (missing `ARTEX_PG_DSN`, no PG in this lab environment; server package only) + 1 Ã— `TestSkillRelPath` (Windows path separators `\` vs `/` â€” pre-existing platform issue) |

Pre/post change: identical failure set (the environmental failures existed before the changes; `server` test files were never touched â€” `git status --short` lists no modifications to `server/*_test.go` or `report/*`).

---

## 13. Known gaps and remaining UNKNOWNs

1. **Traffic proxy allowlist not populated in production** â€” `SetEgressAllowlist`/`SetEgressAddressPolicy`/`SetEgressAuditSink` have no production caller (unit-tested only). Default = sealed-empty = deny-all (fail-closed). When an operator populates it, allowlisted destinations are reachable via the pinned forwarder (tested). SUGGESTION: wire the allowlist from trusted config/env (`ARTEX_EGRESS_ALLOWLIST`?) â€” follow-up.
2. **Race freedom UNKNOWN** â€” `-race` needs cgo; no gcc.
3. **TLS SNI runtime proof** â€” covered by TLS unit tests (pinned + hostname-mismatched cert); TLS through the full proxy (Live D direct fetch bypasses proxy) not re-proven live.
4. **IPv6 runtime** â€” `[::1]` loopback proven in unit tests (`TestDoHTTP_IPv6LiteralPinnedDial`, `TestDoHTTP_IPv6LiteralUnauthorizedZeroDials`); real-world IPv6 destinations untested (no global IPv6 here).
5. **Provider retries (norma)** â€” re-authorization of retried requests is caller-driven; retry behavior after Gate B not re-tested against norma.
6. **Traffic proxy pooling** â€” HTTP/2 off per-connection; connection reuse across requests disabled by `DisableKeepAlives` (provider), forwarder closes after tunnel; no concurrency test vs. the proxy itself.
7. **Server-side `POST /api/llm` denies without response status capture** â€” the 400 responses were observed via bodies (status capture seam); verified the denial text is present (`not authorized`).
8. **Cross-profile scope isolation for `RootDomain`/`Special-Use` at runtime** â€” unit-tested; no runtime traffic scenario (all live destinations were loopback).

---

## 14. Limitations (honest reporting)

- No public destinations were dialed anywhere: all runtime checks used loopback (19001/19002/19004) and the lab PG on 5433; no external DNS (the one non-loopback artifact `TestProviderTLS_SNIAndCertificateVerification` uses `trusted.example`/`other.example` as self-signed fixture names resolved to loopback via the injected resolver; `TestProviderDNS_*` resolvers are injected fakes).
- We did not prove "no unauthorized dials" to public IPs because none were dialed by design; the deny seams are proven by unit/live zero-dial assertions as cited.
- `Live D` proxy denial was verified with a raw proxy probe; `Invoke-WebRequest -Proxy` bypassed the proxy for loopback targets (PowerShell behavior), so the 200 seen there is the direct fetch.
- The original prior-phase live runtimes (8787, 8788, 8799â€“8808) were not touched; the old modified tree remains uncommitted as before (VIII.4.6 changes preserved).

---

## 15. Artifacts

- Fresh binary: `<WORKSPACE>\lab846\artex-v5-846.exe` (SHA256 `0C71DFB19AB3F7C10F9262BDB7D4BAB8BA70A07086E371AE9977854B56BD681E`).
- Lab PG cluster: `<WORKSPACE>\pg\cluster846` (fresh, PostgreSQL 18.7.0); DB `artex_v46h` created in it and left in place (fresh lab DB; no pre-existing database was touched).
- Runtime data dirs: `lab846\data1`; logs: `lab846\run.err` (captures `[trusted] provider hosts=[127.0.0.1:19999]`/`[127.0.0.1:19004]`).
- Stage scripts: `lab846\stage1_check.ps1`, `lab846\stage2_check.ps1`; fixture/probe scripts: `lab846\proxy_probe.py`, `lab846\audit_query2.py`.
- New/changed source: `agent/provider_egress.go`, `agent/provider.go`, `agent/provider_transport_test.go`, `agent/provider_dns_test.go` (Gate A fixes/tests), `agent/target_boundary_test.go` (Gate C), `traffic/egress_dial.go` + `traffic/egress_dial_test.go`, `traffic/egress.go`, `traffic/traffic.go` (Gate D), `agent/bash_guard.go`, `agent/bash_guard_test.go`, `agent/worker.go`, `agent/planner.go`, `agent/mainagent.go`, `agent/chat.go`, `server/assembly.go`, `server/server.go` (Gate E + Gate A runtime wiring, `[trusted]` log).
- Master report: this file.

---

## 16. Definitions

- **Pinned dial (provider)**: DNS-resolved once per dial after exact-host + port + scheme + prefix checks; dial goes to the validated IP (per-dial revalidation closes rebinding).
- **Pinned dial (traffic)**: `egressForwarder` re-resolves and re-validates per CONNECT; mixed answers and resolver failures deny; the dial hits the validated IP.
- **Sealed (traffic)**: empty allowlist denies all upstream (`EgressAllowed` false when sealed and empty).
- **Scope (targets)**: fail-closed `CheckScopeForKey` at the single transport choke point.

---

## 17. Bottom line

All Gates Aâ€“H have boundary-level control enforced, with deterministic local proof. The VIII.4.6.1 defect (untrusted same-IP destination dialable) is fixed and its regression test passes. The traffic proxy denies everything with the current fail-closed default (documented in Â§8/Â§14); live runtime checks confirm denial (502) and acceptance of trusted provider BaseURLs (200). Remaining UNKNOWNs are race freedom, runtime IPv6/TLS, traffic pooling, and production allowlist wiring â€” all explicitly labeled as such; nothing claimed complete.
