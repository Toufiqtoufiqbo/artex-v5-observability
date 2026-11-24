# PHASE8_4_4_1 — PROVIDER RUNTIME POLICY (Phase B)

## 1. Critical fix: trusted policy independent of BaseURL

`agent/provider_egress.go`:

```go
var (
	providerEgressMu      sync.RWMutex
	providerEgressOverride *ProviderEgressPolicy    // (test-only)
	providerEgressTrusted  *ProviderEgressPolicy    // VIII.4.4.1 trusted
)

// SetTrustedProviderPolicy installs the independently trusted provider
// destination policy (VIII.4.4.1). It takes precedence over the
// BaseURL-derived policy.
func SetTrustedProviderPolicy(p ProviderEgressPolicy, enabled bool)
func trustedProviderPolicyLoaded() *ProviderEgressPolicy

func providerEgressPolicyFor(baseURL string) (ProviderEgressPolicy, bool) {
	if p := trustedProviderPolicyLoaded(); p != nil { return *p, true }   // trusted wins
	return ProviderEgressPolicy{ExpectedHost: urlHostName(baseURL), ...}, true
}
```

So if an untrusted configuration changes `BaseURL` to `attacker-controlled-host`, the trusted policy still authorizes only the originally trusted host.

## 2. Transport enforcement verified

`agent/provider.go`:
- `NewProvider()` → `quotaAwareHTTPClient(c.Proxy, c.SessionHeaderKey, c.BaseURL)`.
- `quotaAwareHTTPClient` returns `&http.Client{Transport: providerEgressCheckTransport{base: quotaAwareTransport{...}, policy: policy}}`.
- `providerEgressCheckTransport.RoundTrip` → `CheckProviderEgress(req, policy)` → deny or `t.base.RoundTrip(req)`.

Verified via `TestProviderEgressCheckTransport` (approved host 200; denied hosts: evil.example, http scheme, wrong port, subdomain-suffix, 127.0.0.1) and `TestProviderEgressCheckTransportRedirects`.

## 3. Provider Egress tests (fake transports; `agent/provider_egress_test.go`)

- `TestProviderEgressPolicyFromConfig` — NVIDIA BaseURL → host/port/scheme.
- `TestProviderEgressCheckTransport` / `...Redirects`.
- `TestProviderEgressGlobalOverride` (VIII.4.4, now trusted).
- `TestProviderEgressTrustedPolicyIndependentOfBaseURL` — **new**: `SetTrustedProviderPolicy{trusted.example}` + `NewProvider` with `BaseURL=https://attacker.example/v1` → policy host = `trusted.example`; request to `trusted.example` allowed; request to `attacker.example` denied.

## 4. Runtime status

- New binary `artex-v5-845.exe`; provider config unchanged (NVIDIA, see `PHASE8_4_4_1_RUNTIME_VALIDATION.md`).
- Live provider policy activation (calling `SetTrustedProviderPolicy` from a config source) is a follow-up (§5).
- Unit tests prove independence; no live NVIDIA calls in this phase.

## 5. Follow-up: trusted config source

`SetTrustedProviderPolicy` has no production caller yet. Wiring: read from deployment config (env/flag/DB setting) at startup in `cmd/artex` or `server/manager`, before any provider is used. Documented in `PHASE8_4_4_1_POLICY_INITIALIZATION.md`.

## 6. Domain independence (Phase B)

- Provider policy does NOT read target scope (`doHTTP`/`scopeGuard` untouched by this phase).
- Target scope does NOT authorize provider traffic.
- `SetTrustedProviderPolicy` is typed as `EgressCategory = EgressLLMProvider` in audit events.

## 7. Files

- `agent/provider_egress.go` — trusted policy + transport.
- `agent/provider.go` — transport wrap (VIII.4.4).
- `agent/provider_egress_test.go` — +1 test.
- `agent/provider_capture_test.go` (VIII.4.4) — unchanged this phase.
