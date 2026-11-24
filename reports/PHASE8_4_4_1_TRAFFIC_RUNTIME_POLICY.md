# PHASE8_4_4_1 — TRAFFIC RUNTIME POLICY (Phase A)

## 1. Implementation

`traffic/traffic.go` — `Open()` (production construction path):

```go
t := &Traffic{dir: dir, addr: addr, db: db, closed: make(chan struct{})}
// VIII.4.4.1 — fail-closed production default: the proxy starts sealed.
// An empty/absent allowlist denies all upstream destinations until
// SetEgressAllowlist is called by the trusted runtime configuration.
t.egressSealed.Store(true)
```

`traffic/egress.go` — `SetEgressAllowlist(hosts, sealed)` + `EgressAllowed` (sealed semantics from VIII.4.4).

## 2. Semantics (VIII.4.4.1 table)

| State | Decision |
|---|---|
| New Traffic instance (via `Open`) | **DENY all** (sealed, empty allowlist) |
| Uninitialized policy | DENY |
| `sealed=true, empty allowlist` | DENY ALL |
| `sealed=true, valid allowlist` | ALLOW listed only |
| Malformed allowlist | DENY |
| Legacy permissive | **Not available in production** (`Open` seals; tests can opt out) |
| Runtime configuration unavailable | DENY (proxy is up but denies) |

## 3. Runtime integration (Phase A)

- Policy is established in `Open()` before the listener accepts (`entry.start()` — the proxy begins serving with the sealed state).
- `EgressAllowed` is consulted by the upstream hook in `Open()` at every dial; denial returns an error → `getUpstreamConn` → request aborted before dialing.
- No implicit production path to `sealed=false`: `SetEgressAllowlist` requires explicit call; `Open()` sets `sealed=true` unconditionally.
- Concurrency: `egressSealed atomic.Bool` + `egressAllowlist atomic.Value` (map) — readers take one snapshot per decision (`EgressAllowed` reads sealed first, then map).
- Audit events: `emitTrafficEgressAudit` on allow/deny (already in VIII.4.3).

## 4. Tests (all PASS)

- `TestTrafficProxyEgressDefaultSealed` — `Open()` → `egressSealed` true; `EgressAllowed("http://127.0.0.1:3004/")` false.
- `TestTrafficProxyEgressAllowlist` / `EmptyRestores` / `TestTrafficEgressSealedEmptyDeniesAll` — sealed semantics.
- Runtime proof (Phase H): `phase835\traffic_runtime_check.go` against the new binary's proxy → `status=502` (denied, no successful egress).

## 5. Remaining follow-up

- A runtime configuration source to call `SetEgressAllowlist(trusted, true)` (env/config/API). Until wired, the traffic proxy on production paths denies everything (by design).

## 6. Files

- `traffic/traffic.go` — `Open()` seals.
- `traffic/egress.go` — sealed semantics (VIII.4.4).
- `traffic/egress_test.go` — `TestTrafficProxyEgressDefaultSealed` etc.
- Runtime: `artex-v5-845.exe` (binary), `v5-845.log`.
