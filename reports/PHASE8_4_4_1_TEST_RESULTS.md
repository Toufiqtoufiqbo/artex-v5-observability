# PHASE8_4_4_1 — TEST RESULTS (Phase G)

```
go test ./traffic/ ./agent/ ./enrich/ ./selfupdate/ -count=1 -timeout 180s
  → ok traffic (9.059s), ok agent (0.378s), ok enrich (0.197s), ok selfupdate (0.035s), exit=0
go test ./agent/ -run 'TestProviderEgress' -count=1 -v
  → PASS (TestProviderEgressPolicyFromConfig, ...CheckTransport, ...CheckTransportRedirects,
    ...GlobalOverride, ...TrustedPolicyIndependentOfBaseURL)
go test ./traffic/ -run 'TestTrafficProxyEgress|TestTrafficEgress' -count=1 -v
  → PASS (Allowlist, EmptyRestores, SealedEmptyDeniesAll, DefaultSealed)
go test ./agent/ -run 'TestEgressEventsEmitOnAllowAndDeny' -count=1 → PASS
go build ./... → exit=0
go vet ./agent/ ./traffic/ ./enrich/ ./selfupdate/ → clean
git diff --check → exit=0
```

## VIII.4.4.1-specific

| Test | Result |
|---|---|
| `TestTrafficProxyEgressDefaultSealed` | PASS — `Open()` → sealed; denies all |
| `TestProviderEgressTrustedPolicyIndependentOfBaseURL` | PASS — trusted policy beats BaseURL |
| Phase H runtime check (traffic proxy through new binary) | PASS — denied (502) |

## VIII.4.1/2/3/4 regression

Re-run earlier phases' suites: PASS (see VIII.4.4 `POLICY_REGRESSION.md` + VIII.4.4.1 below).

## Disabled/limited

| Path | Status |
|---|---|
| Enrichment enablement path | follow-up (VIII.4.4.4.1) |
| Traffic proxy runtime config wiring | follow-up |
| Provider trusted-policy deployment wiring | follow-up |
| ShellSessionTools containment | follow-up |
| DNS-to-dial pinning | out of scope (documented) |
