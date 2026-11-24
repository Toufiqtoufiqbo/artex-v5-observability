# PHASE8_4_4 — ENRICHMENT TLS + EGRESS (Phase B)

## 1. TLS correction (mandatory)

`enrich/enrich.go` `buildClient`:
- **Before:** `tls.Config{InsecureSkipVerify: true}` on the enrichment HTTP client.
- **After:** `TLSClientConfig: nil` → standard Go certificate and hostname verification enabled.
- Removed the now-unused `crypto/tls` import.

**Reachability of the bypass (fixed):** enrichment HTTP/HTTPS probes of discovered assets (`doHTTP` in `enrich.go`, client `e.client` built by `buildClient`). Redirect policy unchanged (`ErrUseLastResponse`).

## 2. Egress authorization (mandatory)

`enrich/egress.go` (new) + Engine fields (`egressMu`, `egressAllowed`, `egressEnabled`):

```go
func (e *Engine) SetEgressPolicy(allowedHosts []string, enabled bool)
func (e *Engine) jobEgressAllowed(host string) bool
```

Semantics (fail-closed):
- Default (no policy set): `jobEgressAllowed` → false → **all enrichment jobs are dropped.** Enrichment is disabled until an explicit policy is set.
- `SetEgressPolicy([]string{hosts}, true)` — only listed hosts authorized.
- `SetEgressPolicy(nil, true)` or `SetEgressPolicy(x, false)` — deny all.

Gate: `enrich/enqueue` drops unauthorized jobs with a log line.

**Runtime note:** the server does not yet call `SetEgressPolicy` — enrichment is default-disabled (VIII.4.4). Follow-up: wire an allowlist (e.g., discovered asset hosts) or disable the engine.

## 3. Local TLS tests (`enrich/egress_test.go`)

- `TestEnrichJobEgressPolicy` — default denies; explicit-empty denies; authorized host allowed; non-allowed denied; subdomain of unlisted root denied; empty host denied.
- `TestBuildClientTLSVerification` — `httptest.NewTLSServer` (self-signed): `client.Get` fails with a certificate error (x509/certificate).

```
go test ./enrich/ -count=1 → ok
```

## 4. Follow-ups

- Wire enrichment egress policy to discovered-asset hosts or disable the engine.
- Add a trusted-CA configuration option (not the phase's "don't weaken TLS" direction).
