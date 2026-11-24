# PHASE8_4_4 — PROXY ALLOWLIST (Phase A)

## 1. Semantics implemented

`traffic.Traffic.SetEgressAllowlist(hosts []string, sealed bool)` — Phase VIII.4.4:

| State | Meaning |
|---|---|
| Unsealed (`sealed=false`) | Legacy permissive: `EgressAllowed` returns true for everything. Used by back-compat/tests only. |
| Sealed, empty allowlist | **Deny all** (fail closed). |
| Sealed, allowlist set | Only listed destinations allowed (`host` or `host:port` entries; case-insensitive; both forms accepted). |

`EgressAllowed(req)` (`traffic/egress.go`):
```go
if !t.egressSealed.Load() { return true }           // legacy
host := egressDestHost(req)
if host == "" { return false }                       // no destination → deny
if _, ok := m[host]; ok { return true }              // exact host:port
if h := hostOnly(host); h != "" && h != host { if _, ok := m[h]; ok { return true } }
return false
```

## 2. Enforcement point

`traffic/traffic.go` `Open()` upstream hook → `getUpstreamConn` returns an error on deny → the connection is never dialed. Gated for intercepted requests, CONNECT tunnels and transparent-passthrough.

## 3. Critical question resolved: "Uninitialized" vs "explicitly empty"

- Uninitialized = `sealed=false` → permissive (legacy).
- Explicitly empty-but-sealed = `SetEgressAllowlist(nil, true)` → deny all.
- Distinguished by the sealed flag + stored map state (verified by `TestTrafficEgressSealedEmptyDeniesAll` and `TestTrafficProxyEgressAllowlistEmptyRestores`).

## 4. Runtime status

The new runtime's traffic proxy binds `127.0.0.1:8807` (VIII.4.3 proxy). **The live traffic proxy has no allowlist configured** (unsealed → legacy permissive) — the allowlist is not wired to a runtime API in this phase; it is unit-verified and exposed via `SetEgressAllowlist` for the next phase.

## 5. Tests (all PASS)

- `TestTrafficProxyEgressAllowlist` — allowed host reaches fixture (body asserted); non-allowlisted host denied.
- `TestTrafficProxyEgressAllowlistEmptyRestores` — sealed/unsealed semantics; port-exact match; subdomain denial; case-insensitivity.
- `TestTrafficEgressSealedEmptyDeniesAll` — sealed-empty denies all, including empty host.

```
go test ./traffic/ -count=1 -timeout 120s → ok
```

## 6. Follow-ups

- Wire `SetEgressAllowlist` to a runtime config (DB setting or CLI flag).
- Consider denying-by-default on production starts once wired.
