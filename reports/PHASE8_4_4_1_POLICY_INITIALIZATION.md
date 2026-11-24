# PHASE8_4_4_1 — POLICY INITIALIZATION (Phase E)

## 1. Startup order (documented)

```
Configuration Load
→ Configuration Validation
→ Trusted Policy Construction
→ Policy Validation
→ Policy Activation
→ Service Listener Startup
```

Phase VIII.4.4.1 adds "Trusted Policy Construction" before "Policy Activation".

## 2. Traffic proxy (Phase A)

`traffic.Traffic.Open()` — the production constructor — now sets `egressSealed=true` before accepting connections. A new `Traffic` instance starts sealed: `EgressAllowed` → false for all destinations until `SetEgressAllowlist(hosts, true)`.

Audit-verified: `TestTrafficProxyEgressDefaultSealed` + live runtime check (below).

## 3. Provider (Phase B)

`agent.SetTrustedProviderPolicy` is the trusted policy source. Precedence:

1. Trusted policy (independent of BaseURL).
2. BaseURL-derived policy (fallback).

`providerEgressCheckTransport` wraps the actual transport used by the live Norma client (`agent/provider.go` `NewProvider` → `quotaAwareHTTPClient`).

Wiring to a real deployment source (env/flag/DB) is follow-up.

## 4. Enrichment

Default disabled (`enrich.Engine.egressEnabled=false`); `enrich.New` does not enable it. Enablement via `SetEgressPolicy` is follow-up.

## 5. Fail-closed paths

- If runtime policy is missing, subsystems deny: traffic proxy denies all; provider transport denies (missing host); enrichment denies jobs.
- A policy-initialization failure never enables network execution (the error path returns deny).
- Audit sink failure doesn't grant authority (`emitEgressEvent` is observational; state is enforced elsewhere).

## 6. Isolated-runtime proof

`phase835\traffic_runtime_check.go` against `artex-v5-845.exe` on 127.0.0.1:8808 (proxy 8810):

```
status=502 body=""
BLOCKED (no successful egress)
```

The new runtime's traffic proxy denies egress to `http://127.0.0.1:3004/ok` — proves sealed-by-default activation.

## 7. Files

- `traffic/traffic.go`, `traffic/egress.go`, `traffic/egress_test.go`
- `agent/provider_egress.go`, `agent/provider.go`, `agent/provider_egress_test.go`, `agent/provider_capture_test.go`
- `enrich/enrich.go`, `enrich/egress_test.go`
- `selfupdate/egress.go`, `selfupdate/egress_test.go`
