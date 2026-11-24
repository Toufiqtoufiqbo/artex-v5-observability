# PHASE8_4_4_1 — ENRICHMENT POLICY (Phase C)

Phase VIII.4.4 delivered enrichment TLS-on + `SetEgressPolicy` (default disabled). VIII.4.4.1 keeps this contract and activates it fail-closed.

## 1. Status

- **Default: DISABLED.** `enrich.Engine` has `egressEnabled=false` until `SetEgressPolicy(hosts, true)` is called.
- **TLS: ON.** `enrich/enrich.go` `buildClient` uses `TLSClientConfig: nil` (standard verification; the `InsecureSkipVerify` bypass is removed).
- **Policy: enforced at `enqueue`** (`enrich/enrich.go`): unauthorized jobs are dropped with an audit log line.

## 2. Runtime verification (unit)

- `TestEnrichJobEgressPolicy` — default denies; explicit-empty denies; authorized allowed; non-listed/subdomain/empty denied.
- `TestBuildClientTLSVerification` — self-signed server → certificate error.

## 3. Follow-up (activation)

`SetEgressPolicy` currently has no production caller (Phase VIII.4.4 delivery was tested, not wired). Wiring a trusted allowlist (discovered-asset hosts) or keeping enrichment disabled is the follow-up documented in `PHASE8_4_4_1_POLICY_INITIALIZATION.md`.

## 4. Files

- `enrich/enrich.go` (VIII.4.4)
- `enrich/egress_test.go` (VIII.4.4)
