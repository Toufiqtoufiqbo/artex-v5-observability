# PHASE8_4_5.2 — IMPLEMENTATION

Scope: main target-assessment HTTP client (`agent/doHTTP`).

## 1. Changes

### `agent/scopecheck.go` (new funcs)

```go
func addressAuthorized(key string, ip net.IP, host string) bool
```
Address policy: `ip` entries (scopeEntryIP: Value/Net) must Equal; `cidr` entries must contain; `root_domain`/`subdomain` entries authorize when they match the hostname (same semantics as CheckScope). Uses `getScope(key)`.

### `agent/httpclient.go`

```go
func resolvePinnedIP(ctx, key, host) (net.IP, error)   // net.DefaultResolver.LookupIPAddr + addressAuthorized per candidate
func dialPinnedContext(ctx, network, addr) (net.Conn, error)  // pinned dial
func splitHostPort(addr) (host, port string)
```

- Literal-IP targets: validated by `addressAuthorized`; dialed via `dialFn` with the original `addr`.
- Hostname targets: `net.DefaultResolver.LookupIPAddr` → first address with `addressAuthorized(key, a.IP, host)` → `dialFn(ctx, network, net.JoinHostPort(ip.String(), port))` — **the actual TCP connection goes to the validated IP**; the URL host is preserved for Host/TLS (Go's Transport sets TLS ServerName from the request URL).
- Wired: `tr := &http.Transport{..., DialContext: dialPinnedContext}` (VIII.4.4 transport kept: `egressHTTPProxyPolicy`).
- Scope context: `key := scopeKeyFromContext(ctx)` (same key as scopeGuard — VIII.4.2 per-task store).

## 2. Tests (`agent/scope_pin_test.go`, all PASS)

- `TestAddressAuthorized_IPEntry` — 127.0.0.2/32 authorized; 127.0.0.3 not.
- `TestAddressAuthorized_RootDomain` — root_domain example.com authorizes subdomain addr.
- `TestDoHTTP_DialsAuthorizedIP` — scope `ip Net=hostOf(srv)+/32`; doHTTP → 200 (pinned dial works).
- `TestDoHTTP_LiteralUnauthorizedDenied` — out-of-scope literal → scope denial.

## 3. Execution evidence

```
go test ./agent/ -run 'TestAddressAuthorized|TestDoHTTP_DialsAuthorizedIP|TestDoHTTP_LiteralUnauthorizedDenied' -count=1 -v → PASS (4/4)
go test ./agent/ -count=1 -timeout 180s → ok github.com/Autumn-27/artex/agent
go build ./... → exit=0
go vet ./agent/ → clean
```

## 4. Current ACTUAL_DIAL_DESTINATION_VERIFIED status (VIII.4.5.1 §3)

- `ip`/`cidr` scopes: **VERIFIED** — dialed IP is an address-authorized entry (literal or resolved).
- `root_domain`/`subdomain` scopes: **PARTIAL** — the dialed address is the first resolved address that matches the hostname-kind entry; the hostname match re-authorizes (same as CheckScope). The pinning to the resolved set is the intended property, but DNS rebinding between CheckScope and dial is not fully eliminated for hostname-kind scopes.

## 5. Files

- `agent/scopecheck.go` — `addressAuthorized`.
- `agent/httpclient.go` — `resolvePinnedIP`, `dialPinnedContext`, `splitHostPort`, transport `DialContext` swap.
- `agent/scope_pin_test.go` — 4 tests.

## 6. Verdict (implementation scope)

`PARTIAL — DESTINATION PINNING LIMITED` (hostname-kind scopes not strictly pinned).

## 7. Next: VIII.4.5.2.1 — strict hostname-pinning for hostname scopes? Or accept the documented limitation.
