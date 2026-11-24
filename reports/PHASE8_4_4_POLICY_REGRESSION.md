# PHASE8_4_4 — POLICY REGRESSION (Phase F)

Re-ran VIII.4.1/2/3 suites after VIII.4.4 changes:

```
go test ./agent/ ./traffic/ ./enrich/ ./selfupdate/ -count=1 -timeout 180s → ok/ok/ok/ok (exit=0)
go test ./agent/ -run 'TestScopeIsolation|TestBashGuard|TestWorkerDefaultTools_ContainsBash|TestDropMCPTools|TestProviderEgress|TestEgressEventsEmitOnAllowAndDeny' -count=1 -v → PASS
go test ./traffic/ -run 'TestTrafficProxyEgress|TestTrafficEgress' -count=1 → PASS
go test ./enrich/ -count=1 → ok
go test ./selfupdate/ -count=1 → ok
```

## 1. Domain separation (verified)

| Domain | Policy | VIII.4.4 additions |
|---|---|---|
| Target assessment | Task scope + bash guard + doHTTP scopeGuard | — (preserved) |
| LLM provider | `providerEgressCheckTransport` (VIII.4.4) + disabled env proxy | deny-by-policy |
| Internal services | unchanged | — |

No cross-domain inheritance:
- Provider-egress transport does not read target scope.
- Target `doHTTP` does not read provider policy.
- Enrichment/selfupdate/traffic do not read target scope.

## 2. VIII.4.4-specific regression

- `go vet ./agent/ ./traffic/ ./enrich/ ./selfupdate/` → clean.
- `git diff --check` → exit 0.
- provider_capture tests updated to pass a valid `baseURL` (127.0.0.1:port) — captured behavior assertions unchanged.

## 3. Regression verdict (Phase F)

`PASS` — VIII.4.1/2/3 guarantees intact; VIII.4.4 adds provider-egress, enrichments TLS+policy, traffic sealed allowlist, self-update disable.
