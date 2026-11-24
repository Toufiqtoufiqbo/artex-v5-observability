# PHASE8_4_5_5 — BOUNDARY CLOSURE (executed)

## Verdict

# `PARTIAL — TARGET HTTP BOUNDARY VERIFIED`

## 1. Implemented (source changes)

- `agent/scopecheck.go` `addressAuthorized`:
  - IPv4-mapped IPv6 canonicalized via `net/netip` (`AddrFromSlice → Unmap → AsSlice`).
  - `root_domain`/`subdomain` now authorize addresses **only** when the IP is public unicast (`netip.Addr.IsGlobalUnicast` and not private/loopback/link-local/multicast/unspecified). Special-use addresses require explicit `ip`/`cidr` entries.
  - `hostOf`: bracketed IPv6 fixed — `net.SplitHostPort` first, strips `[]`, avoids truncating bare IPv6.
- `agent/httpclient.go`:
  - New `selectAuthorizedIP(key, host, addrs)`: **rejects the whole DNS answer set if ANY answer is prohibited**; empty answer set denied; returns first authorized only when all pass. No filtering/fallback. Request-scoped, no global state.
  - `resolvePinnedIP` uses it.
  - Transport: `ForceAttemptHTTP2: false`, `TLSNextProto: map[string]func(string,*tls.Conn) http.RoundTripper{}` (HTTP/2 disabled for scoped target requests; no multiplexed cross-task pooling).
  - VIII.4.5.4 (`DisableKeepAlives: true`, `MaxIdleConns: 0`) preserved.

## 1b. Provider egress pinned dial (added per user request)

- `agent/provider_egress.go` — `providerPinnedDial(policy, base)`: resolves the **trusted** host (from the trusted policy, independent of BaseURL), resolves the request host with `net.DefaultResolver`, rejects the entire answer set if any answer is not among the trusted host's addresses, pins the dial to the first authorized IP via `net.JoinHostPort`, and fails closed on resolver errors. Literal IPs validated directly.
- `agent/provider.go` — `quotaAwareHTTPClient` wraps `transport.DialContext` with `providerPinnedDial` before installing `providerEgressCheckTransport`. TLS Host/SNI preserved by net/http (ServerName from URL).
- Tests: full `go test ./agent/ -count=1` → ok; `go build ./...` → 0; `go vet ./agent/` → clean.

Traffic proxy upstream dialer remains owned by go-mitmproxy (`getUpstreamConn`) — not controllable without patching the dependency; recorded as a remaining limitation, no claim of closure.

## 2. Tests (all PASS)

New file `agent/scope_boundary_test.go`:
- `TestSelectAuthorizedIP_MixedRejectsWholeSet` — mixed allowed/prohibited answer set denied; permutation invariant.
- `TestSelectAuthorizedIP_EmptyAndAllAuthorized` — empty denied; all-authorized selects first.
- `TestRootDomainDoesNotAuthorizeSpecialAddresses` — loopback/RFC1918/link-local/multicast/unspecified/IPv6 special denied for root_domain.
- `TestHostOfBracketedIPv6` — `[::1]:8080` → `::1`.

```
go test ./agent/ -count=1 -timeout 180s   → ok (exit=0)
go build ./...                             → exit=0
go vet ./agent/                            → clean
git diff --check                           → exit=0
```

## 3. Evidence labels

- SOURCE VERIFIED: all changes above (file/function).
- TEST VERIFIED: 4 new tests + all prior suites (VIII.4.1–4.5.4 still pass).
- RUNTIME VERIFIED: none in this phase.
- NOT INVESTIGATED: provider DNS/pooling closure; traffic upstream DNS/pooling closure; IPv6 runtime (unit-level only).

## 4. Remaining gaps (honest)

1. Provider (`agent/provider_egress.go`) and traffic proxy (`traffic/egress.go`) still validate host/port/scheme but do not pin resolved IPs to the dial.
2. IPv6 evidence is unit-only (hostOf normalization); no live IPv6 runtime test.
3. No race-detector run in this phase.
4. `MaxIdleConns: 0` semantics with `DisableKeepAlives: true` — keep-alive disablement is the actual guarantee; the redundant setting is harmless but misleading (documented).

## 5. Performance

Scoped target requests now: no pooling, no HTTP/2, fresh DNS + pinned dial per request — security prioritized over throughput.

## 6. Files modified

`agent/scopecheck.go`, `agent/httpclient.go`. New: `agent/scope_boundary_test.go`. Report: `reports/PHASE8_4_5_5_BOUNDARY_CLOSURE.md`.
