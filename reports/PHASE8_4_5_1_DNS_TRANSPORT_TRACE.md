# PHASE8_4_5.1 — DNS & HTTP TRANSPORT FORENSIC MAPPING

Investigation only. No source modifications. No live network tests. No DNS implementation.

## 1. Main target HTTP execution chain

```
Worker.execute → Tool (runHTTPRequest etc.)
  → doHTTP(ctx, httpCall{URL: rawURL, ...})            agent/httpclient.go:133
    → scopeGuard(ctx, rawURL)                           agent/httpclient.go:147
      → CheckScopeForKey(scopeKeyFromContext(ctx), rawURL)   agent/scopecheck.go (wrapper scopeGuard)
        → validateScopeEntries + hostOf + match         agent/scopecheck.go (CheckScopeForKey)
      [HOST_AUTHORIZED if allowed]
    → url.Parse(rawURL)                                 agent/httpclient.go:139
    → http.NewRequestWithContext(ctx, method, rawURL, body)  agent/httpclient.go:166
    → tr := &http.Transport{ ..., DialContext: dialFn }  agent/httpclient.go:198-201
        (transport construction; Proxy: egressHTTPProxyPolicy — VIII.4.4)
    → client := &http.Client{Transport: tr, ...}         agent/httpclient.go:202-205
    → client.Do(req)                                     agent/httpclient.go:239
        (net/http: resolves URL hostname via its transport dial, then dialFn)
      → dialFn(ctx, network, addr)                       agent/httpclient.go:129
        → defaultDialContext                             agent/httpclient.go:129-131
          → (&net.Dialer{Timeout: 15s}).DialContext(ctx, network, addr)   agent/httpclient.go:130
            → Go stdlib DNS resolution → net.Conn (RemoteAddr = actual IP)
```

Redirect revalidation (VIII.4.3/8.4.3): `CheckRedirect` → `scopeGuard(ctx, req.URL.String())` — `agent/httpclient.go:223`.

## 2. DNS resolution ownership

- ARTEX does NOT call a resolver directly. No `net.Resolver` use in the reviewed path.
- DNS resolution is performed by Go's standard library: inside `net.Dialer.DialContext` (`agent/httpclient.go:130`).
- `http.Transport` receives the URL host:port; the transport itself does not pre-resolve when a custom `DialContext` is set — it calls the dialer with the logical address.
- `scopeGuard` runs **before** `client.Do` → before DNS.

**DNS resolution owner: `net.Dialer` (Go stdlib), invoked from `defaultDialContext` (`agent/httpclient.go:129`).**

## 3. Authorization-to-dial relationship

| Stage | Established? | Source |
|---|---|---|
| `HOST_AUTHORIZED` | ✅ SOURCE VERIFIED | `CheckScopeForKey` — `agent/scopecheck.go` (match loop: root_domain/subdomain/ip/cidr). |
| `RESOLVED_ADDRESS_AUTHORIZED` | ❌ NOT ESTABLISHED | No ARTEX code resolves the hostname; `CheckScopeForKey` matches the hostname, never an IP. |
| `ACTUAL_DIAL_DESTINATION_VERIFIED` | ❌ NOT ESTABLISHED | `defaultDialContext` dials the hostname; the actual `RemoteAddr` is never compared to any ARTEX-validated address. |

The three stages are currently: `HOST_AUTHORIZED` = YES; `RESOLVED_ADDRESS_AUTHORIZED` = NO; `ACTUAL_DIAL_DESTINATION_VERIFIED` = NO.

The gap: a hostname can be authorized, and DNS can resolve it to any address at dial time (rebinding). There is no ARTEX-level pinning of the dialed IP.

## 4. Hostname/IP normalization

`agent/scopecheck.go`:
- `hostOf` — parses `host:port` (LastIndex(":")), strips scheme/path/userinfo. Known limitation: IPv6 with port (`[::1]:8080`) is not parsed correctly (LastIndex(":") yields `[::1]`).
- `scopeEntryIP` — `net.ParseIP`/`net.ParseCIDR` on Value/Net; no IPv6-mapped normalization.

**This is the key gap for the VIII.4.5 work: no IPv6 handling exists in the transport path.**

## 5. Connection reuse and redirect boundaries (existing behavior)

- Connection pooling: standard Go transport; `http.Transport` reuses connections (keep-alive); on reuse, **no dial occurs** — no authorization is re-checked per request (standard Go behavior).
- Redirect: `scopeGuard` revalidates hostname per hop; DNS is re-resolved on a new connection; reused connections bypass scopeGuard (Go behavior).

## 6. Secondary client inventory (source-level)

| Subsystem | Transport | Scope/Egress control | Status |
|---|---|---|---|
| Norma provider | `agent/provider.go` `quotaAwareHTTPClient` → `providerEgressCheckTransport` | Trusted-policy host/port/scheme check (VIII.4.4.1); no IP validation | host-level only (VIII.4.5) |
| Traffic proxy | `traffic/traffic.go` (go-mitmproxy) | Sealed allowlist (VIII.4.4.1); upstream host validation | host-level only (VIII.4.5) |
| Enrichment | `enrich/enrich.go` `buildClient` | Disabled by default (VIII.4.4) | — |
| Self-update | `selfupdate/` | Disabled by default (VIII.4.4) | — |
| ShellSessionTools | norma `tool/shell_session.go` | Not registered for worker; follow-up | see `PHASE8_4_5_SHELLSESSION_CONTAINMENT.md` plan |

## 7. Minimum safe DNS-to-Dial design (VIII.4.5.2 preview)

1. Keep hostname authorization (`CheckScope`) — already exists.
2. Add `net.Resolver` (injected per client; default: system resolver) → `ResolveIPAddresses(ctx, host) ([]net.IP, error)`.
3. Validate each address against the existing address policy (new: `scopeEntryIP` extended or new `addressAuthorized(ip)`):
   - For `ip` entries: entry IP (from Value/Net) must equal the address.
   - For `cidr` entries: `ipnet.Contains(addr)`.
   - Empty DNS result or resolver error → DENY.
4. Dial the validated address directly:
   - Use `net.Dialer.DialContext(ctx, "tcp", net.JoinHostPort(validatedIP, port))`.
   - For TLS: custom `DialTLSContext` (in `http.Transport`) that dials the validated IP but sets `TLSClientConfig.ServerName = originalHost` so Host/SNI stay correct.
   - Or use `net.Dialer.DialContext` with the host:port (which re-resolves — NO, for pinning we must dial the IP).
5. Redirect: re-run authorization + resolution per hop (`CheckRedirect` → `CheckScope` + address validation + dial on new connection).
6. Retries/new connections: re-run authorization per logical request; do not reuse another task's validated destination.
7. IPv6/IPv4-mapped handling: normalize `v4InV6`; handle IPv6 zone identifiers when dialing.
8. Tests (`PHASE8_4_5_DNS_TESTS.md`): fake resolver scenarios 1–12 from the phase (authorized/unauthorized address, rebinding, mixed answers, timeouts, IP literals, IPv4-mapped IPv6, concurrent tasks, redirects).

Files/functions for the next phase: `agent/scopecheck.go` (`scopeEntryIP`, new `addressAuthorized`), `agent/httpclient.go` (`doHTTP`, `dialFn`, new `resolveAndValidate`), `agent/provider_egress.go` (transport), `traffic/egress.go`.

## 8. Current destination-integrity gaps (honest list)

1. **DNS resolution is owned by `net.Dialer`** — ARTEX never resolves or validates addresses (VIII.4.5).
2. **No pinned dial** — the dialed address is not compared to any validated value (VIII.4.5).
3. **Hostname+IP dual-match rule**: existing scope matching is hostname-only (`root_domain`/`subdomain` match hostnames; `ip`/`cidr` match hostnames parsed via `hostOf`).
4. **Connection reuse**: standard Go pooling; no per-request re-authorization on reuse (documented as Go behavior).
5. **IPv6 transport**: not covered (`hostOf` limitation documented).
6. **Redirect: hostname-level revalidation** — no address validation (VIII.4.5).
7. **ShellSessionTools**: backend containment unresolved (VIII.4.5 plan).

## 9. Acceptance facts

| Fact | Status | Evidence |
|---|---|---|
| Main client | `agent.doHTTP` | `agent/httpclient.go:133` |
| Transport | `http.Transport` (custom `DialContext: dialFn`) | `agent/httpclient.go:198-201` |
| Dial | `net.Dialer.DialContext` (system DNS) | `agent/httpclient.go:129-131` |
| Hostname auth | `CheckScopeForKey` | `agent/scopecheck.go` |
| IP auth | **MISSING** | no resolver calls in path |
| Actual dial verification | **MISSING** | no pinning |
| Task authorization at dial | via `scopeGuard` pre-`Do` (hostname only) | `agent/httpclient.go:147` |

## 10. Verdict

# `TRACE COMPLETE WITH DESTINATION LIMITATIONS`

(VIII.4.5 delivered: main HTTP client + transport + dial traced; current gaps listed; next-phase design drafted.)

## 11. Report

`reports/PHASE8_4_5_1_DNS_TRANSPORT_TRACE.md`

## 12. Stop conditions

No source modified; no live network tests run; no services touched.
