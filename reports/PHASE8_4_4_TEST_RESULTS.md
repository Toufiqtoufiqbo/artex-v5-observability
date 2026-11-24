# PHASE8_4_4 — TEST RESULTS (Phase I)

Local-only: fake transports, local loopbacks, no public endpoints, no real provider calls.

## 1. Mandated commands

```
go test ./agent/ ./traffic/ ./enrich/ ./selfupdate/ -count=1 -timeout 180s
  → ok  github.com/Autumn-27/artex/agent     (0.296s)
  → ok  github.com/Autumn-27/artex/traffic    (8.255s)
  → ok  github.com/Autumn-27/artex/enrich    (0.126s)
  → ok  github.com/Autumn-27/artex/selfupdate (0.027s)
  → exit=0
go test ./agent/ -run 'TestProviderEgress' -count=1 -v → PASS (4/4)
go test ./traffic/ -run 'TestTrafficProxyEgress|TestTrafficEgress' -count=1 -v → PASS
go test ./enrich/ -run 'TestEnrichJobEgressPolicy|TestBuildClientTLSVerification' -count=1 -v → PASS
go test ./selfupdate/ -count=1 -v → PASS (TestSelfUpdateDisabledByDefault, TestSelfUpdateBlockedWhenDisabled)
go build ./... → exit=0
go vet ./agent/ ./traffic/ ./enrich/ ./selfupdate/ → clean
git diff --check → exit=0
```

## 2. VIII.4.4 new tests (all PASS)

### Provider (agent)

| Test | Assertion |
|---|---|
| `TestProviderEgressPolicyFromConfig` | NVIDIA config → host/port/scheme; empty BaseURL → deny |
| `TestProviderEgressCheckTransport` | approved host 200; evil host / wrong scheme / wrong port / subdomain-suffix / 127.0.0.1 → denied |
| `TestProviderEgressCheckTransportRedirects` | redirect to unauthorized host denied |
| `TestProviderEgressGlobalOverride` | override semantics |

### Traffic

| Test | Assertion |
|---|---|
| `TestTrafficProxyEgressAllowlist` | allowed host through proxy returns body; non-allowlisted denied |
| `TestTrafficProxyEgressAllowlistEmptyRestores` | sealed/unsealed; port-exact; subdomain denial |
| `TestTrafficEgressSealedEmptyDeniesAll` | sealed-empty denies all |

### Enrichment

| Test | Assertion |
|---|---|
| `TestEnrichJobEgressPolicy` | default/explicit-empty deny; authorized/non-listed/subdomain/empty-host |
| `TestBuildClientTLSVerification` | self-signed server → certificate error (TLS on) |

### Self-update

| Test | Assertion |
|---|---|
| `TestSelfUpdateDisabledByDefault` | disabled at start |
| `TestSelfUpdateBlockedWhenDisabled` | FetchLatest/Stage blocked; toggle works |

## 3. VIII.4.1/2/3 regression (PASS) — see `PHASE8_4_4_POLICY_REGRESSION.md`

## 4. Disabled paths (Phase G)

| Path | Status |
|---|---|
| Enrichment | DISABLED by default (VIII.4.4; enable via `SetEgressPolicy` — follow-up) |
| Self-update | DISABLED by default |
| Bash interactive shell | guarded (heuristic, VIII.4.3) |
| Live traffic-proxy allowlist API | not exposed (unit-verified) |

## 5. Secret scan

Strict scan over `reports/PHASE8_4_4_*.md` for an API key string of length ≥10, the admin password, or bearer-style credentials: **no matches** (the scan pattern itself appears only as documentation text).
