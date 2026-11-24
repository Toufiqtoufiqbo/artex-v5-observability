# PHASE8_4_0 — NETWORK BOUNDARY FORENSIC AUDIT (budget-limited)

**Verdict: `PARTIAL — BUDGET EXHAUSTED`**

## 1. Executive verdict
Partial inventory only; no untraced path is claimed safe. Source was not modified.

## 2. Source identity (SOURCE VERIFIED)
Modified `artex-original` @ `e6ec569` (dirty tree: `agent/tools.go`, `agent/worker.go` modified; 14 new `agent/*.go` + tests). Baseline archive SHA-256 `93AFBEC0…CD11E2`, no git metadata (identity UNPROVEN). Go 1.26.11; norma v0.4.3 vs v0.4.4.

## 3. Network egress inventory (PARTIALLY VERIFIED)
- `agent/httpclient.go`: `doHTTP` — sole `http.Client` for HTTPRequest/Probe/Crawl/Login/Session/IdorDiff/Fuzz/Retest; `dialFn` hook; `ProxyFromEnvironment`; TLS `InsecureSkipVerify: c.Insecure`; redirect guard.
- LLM provider client in `norma` — transport **NOT INVESTIGATED**.
- Traffic MITM proxy :8788 (`traffic/`) — **NOT INVESTIGATED**.
- `exec.Command`/Bash tool (`agent/tools.go` default tools) — **NOT INVESTIGATED** (can originate connections via subprocess).
- Notifications/self-update — **NOT INVESTIGATED**.

## 4. Path coverage
Paths 1–5 (HTTP tools, Crawler, Login, Probe, Retest) route through `doHTTP` (SOURCE VERIFIED via registration in `agent/worker.go`). Paths 6–8 (LLM, proxy, subprocess) UNTRACED.

## 5. Scope policy (PARTIALLY VERIFIED)
`agent/scopecheck.go`: company fail-closed; root_domain/subdomain/ip/cidr; `doHTTP` calls `scopeGuard` before transport. Empty scope: `if !seeded return nil` → **IMPLICIT DEFAULT-ALLOW** (CONFIRMED DEFECT by source). Malformed → deny via no-match.

## 6. Proxy policy (PARTIALLY VERIFIED)
`ProxyFromEnvironment` inherited in `doHTTP` — proxy destination not separately authorized (POTENTIAL RISK). No explicit policy.

## 7. DNS / actual destination (PARTIALLY VERIFIED)
No resolution or IP pinning in inspected code; validation is hostname-only; TOCTOU possible (POTENTIAL RISK). Not fully traced.

## 8. Redirect / retry / reuse (PARTIALLY VERIFIED)
`CheckRedirect`: revalidates destination, 10-hop bound, strips Authorization/Cookie cross-origin (SOURCE VERIFIED; `TestRedirectNoDial` RUNTIME VERIFIED). Retry authorization: NOT INVESTIGATED. Connection reuse: NOT INVESTIGATED.

## 9. Concurrency / cancellation (PARTIALLY VERIFIED)
Scope store protected by RWMutex; dial counter atomic. `-race` unavailable (no gcc) — environmental.

## 10. Test coverage (TEST VERIFIED where executed)
`TestCheckScope_*`, `TestNetworkBoundaryAccounting` (0 dials on reject), `TestRedirectNoDial` — executed, ok. Proxy/DNS/retry/reuse/concurrency tests: NO TEST FOUND.

## 11–12. Confirmed defects / risks
P0: implicit default-allow on empty scope. P1: proxy policy, DNS pinning, untraced LLM/subprocess/proxy egress, retry authorization.

## 13. Unknowns
Norma provider transport; traffic proxy egress; subprocess egress; self-update/notifications; connection reuse; retry middleware.

## 14. Remediation plan (not implemented)
1. Empty-scope default-deny with explicit allow-all opt-in (`scopecheck.go:CheckScope`).
2. Central egress hook wrapping all clients incl. proxy + provider + subprocess policy.
3. Proxy destination authorization before dial.
4. DNS-to-dial pinning with TLS hostname preserved.
5. Retry revalidation; per-request scope check; bounded tests each.

## 15. Phase VIII.4.1 acceptance
Requires: full trace of paths 6–8; empty-scope decision; proxy/DNS/retry/reuse tests; `-race` environment.

No code modified; no services restarted; no external requests.
