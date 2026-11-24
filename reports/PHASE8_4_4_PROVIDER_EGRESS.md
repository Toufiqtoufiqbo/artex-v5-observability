# PHASE8_4_4 — PROVIDER EGRESS (Phase C)

## 1. Implementation (actual boundary)

Norma (`github.com/Autumn-27/norma`) is external and untouched. The enforcement lives in the ARTEX-side transport that the provider client uses:

- `agent/provider_egress.go` (new): `ProviderEgressPolicy{ExpectedHost, ExpectedPort, ExpectedScheme, ExtraValidators}`, `CheckProviderEgress(req, policy)`, `providerEgressCheckTransport`, `SetProviderEgressPolicy(p, enabled)` (global override).
- `agent/provider.go`:
  - `quotaAwareHTTPClient(proxy, sessionHeaderKey, baseURL)` now wraps the client transport: `providerEgressCheckTransport{base: quotaAwareTransport{...}, policy: providerEgressPolicyFor(baseURL)}`.
  - `NewProvider` passes `c.BaseURL` through.

**Enforcement point:** every request the provider client sends passes `CheckProviderEgress` in `providerEgressCheckTransport.RoundTrip` **before** hitting the network. Since the provider client is handed to Norma, this is the actual request boundary.

## 2. Policy semantics (fail-closed)

Policy per provider client (derived from `Config.BaseURL`), or a global override via `SetProviderEgressPolicy`.

For the configured NVIDIA profile: `providerEgressPolicyFor("https://integrate.api.nvidia.com/v1")` = `{ExpectedHost: "integrate.api.nvidia.com", ExpectedPort: "443", ExpectedScheme: "https"}`.

`CheckProviderEgress` rejects:
- Empty/missing expected host (`DENY`).
- Host mismatch (no wildcards, no subdomain acceptance).
- Scheme mismatch (default https).
- Port mismatch (explicit or default 443/80).
- Custom validators (ExtraValidators).

If neither a per-instance policy nor the global override is set, the transport denies every request (missing configuration → DENY).

## 3. Redirects/retries

- http.Client sends redirect requests through the same `RoundTrip` → revalidated per hop.
- Retries in Norma re-enter `RoundTrip` → revalidated.

## 4. Proxy

Provider clients (`quotaAwareHTTPClient`) already use `http.ProxyURL(proxyURL)` for explicit proxies only; env proxies are disabled (`transport.Proxy = nil` when no explicit proxy — `provider.go:404`). Environment proxy variables cannot silently authorize provider traffic.

## 5. Audit

`emitEgressEvent(EgressEventForURL(EgressLLMProvider, 0, url, allow/deny, reason))` on allow/deny (phase F/I events).

## 6. Tests (`agent/provider_egress_test.go`, fake transports)

| Test | Assertion |
|---|---|
| `TestProviderEgressPolicyFromConfig` | `https://integrate.api.nvidia.com/v1` → host/port/scheme as expected; empty BaseURL → empty host |
| `TestProviderEgressCheckTransport` | approved destination → 200; `https://evil.example/`, `http://integrate.api.nvidia.com/`, wrong port (8443), `integrate.api.nvidia.com.evil.net`, `https://127.0.0.1/v1/` → denied |
| `TestProviderEgressCheckTransportRedirects` | 302 to `https://evil.example/x` → denied |
| `TestProviderEgressGlobalOverride` | override host allowed, others denied |

```
go test ./agent/ -run 'TestProviderEgress' -count=1 -v → PASS
```

## 7. Non-verified limitation

- DNS-to-dial pinning is **not** implemented here (`phases`, `WW.4.4` future): the hostname policy is checked before dispatch, but the actual IP dialed is not validated against the hostname at dial time.

## 8. Live runtime check

- New binary `artex-v5-844.exe`: provider config unchanged (`https://integrate.api.nvidia.com/v1`, model `nvidia/nemotron-3.5-lightning-30b-a3b`) — see `PHASE8_4_4_RUNTIME_VALIDATION.md`.
- Unit tests + build prove enforcement; the live provider is not called in this phase (VIII.4.4 rules).
