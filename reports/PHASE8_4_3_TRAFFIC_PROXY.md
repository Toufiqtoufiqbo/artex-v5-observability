# PHASE8_4_3 — TRAFFIC PROXY (Phase C)

## 1. Implementation

`traffic/egress.go` (new):

```go
func (t *Traffic) SetEgressAllowlist(hosts []string)
func (t *Traffic) AllowedEgressHosts() []string
func (t *Traffic) EgressAllowed(req *http.Request) bool
func emitTrafficEgressAudit(host string, allowed bool, reason string)
func SetEgressAuditSink(fn func(host string, allowed bool, reason string))
```

`traffic/traffic.go` — the upstream hook in `Open`:

```go
p.SetUpstreamProxy(func(req *http.Request) (*url.URL, error) {
    host := egressDestHost(req)
    if !t.EgressAllowed(req) {
        err := fmt.Errorf("traffic proxy: egress to %s denied by explicit allowlist", host)
        emitTrafficEgressAudit(host, false, err.Error())
        return nil, err
    }
    emitTrafficEgressAudit(host, true, "")
    return t.upstream.Load(), nil
})
```

Because `getUpstreamConn` (`go-mitmproxy proxy/proxy.go:122`) aborts the connection on hook error, this single chokepoint gates:

- intercepted HTTP requests (`attacker.initHttpDialFn` → `getUpstreamConn`),
- HTTPS CONNECT tunnels (`handleConnect` → `getUpstreamConn`),
- transparent-passthrough (`getUpstreamConn`).

So: **no destination can be dialed unless the allowlist allows it** (when an allowlist is configured).

## 2. Allowlist semantics

- Keys: `"host"` or `"host:port"`, case-insensitive. Both `"host"` and `"host:port"` forms are accepted as matches: a request `host:port` matches an entry `host`, and vice versa.
- Empty/nil list → permissive (previous behavior; backwards compatible).
- Denied hosts produce an explicit error: `traffic proxy: egress to <host> denied by explicit allowlist`.

## 3. Runtime status

The running new binary's traffic proxy binds `127.0.0.1:8803` (verified UP on the isolated runtime). Allowlist for the new runtime: **not set** — it must be configured via `SetEgressAllowlist` before use; the API/CLI surface for it is a follow-up (VIII.4.3 non-goal "full retry authorization" style separation).

## 4. Tests

- `TestTrafficProxyEgressAllowlist` — full proxy path: allowed host succeeds through the proxy and returns the fixture body; non-allowlisted host is denied (no `ok-B`).
- `TestTrafficProxyEgressAllowlistEmptyRestores` — empty allowlist permissive; case-insensitivity; port-exact and subdomain denial.
- `TestTrafficProxyAuditSink` — audit events for allow/deny.

All run through the real proxy via `http.Transport{Proxy: http.ProxyURL(tr.ProxyAddr())}`.

Results: PASS (`go test ./traffic/ -count=1` → `ok github.com/Autumn-27/artex/traffic`).

## 5. Notes

- `SslInsecure: true` is unchanged (local lab proxy).
- Fail-open passthrough is unchanged: only for hosts that fail interception (`maybePassthrough`), and the upstream hook still gates the passthrough dial.
- The traffic proxy's own listener: binds via `-proxy` flag. `ProxyAddr()` maps bare `":port"` to loopback (verified `server/manager.go:378` + `traffic.go:305`).
