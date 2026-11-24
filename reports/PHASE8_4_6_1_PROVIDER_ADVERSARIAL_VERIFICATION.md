# PHASE8_4_6_1 — PROVIDER ADVERSARIAL VERIFICATION (final, with failure)

## Verdict

# `FAILED — UNTRUSTED PROVIDER DESTINATION CAN BE DIALED`

## 1. Confirmed defect (TEST VERIFIED — failing assertion preserved)

`TestProviderDNS_DifferentHostSameIP` (`agent/provider_dns_test.go`):

```
same-IP different-hostname must be denied: fake dial [1.2.3.4:443]
--- FAIL
```

`providerPinnedDialWithResolver` (`agent/provider_egress.go`) authorizes a request hostname **only by comparing its resolved IP set against the trusted hostname's IP set**. An attacker-controlled hostname (`evil.example`) resolving to the same IP as `trusted.example` passes the check and is dialed. This violates the acceptance criterion: "Different hostnames cannot become trusted solely because they resolve to the same IP."

The test assertion was NOT weakened; production logic was NOT repaired during this verification phase.

## 2. Other verified results

- `TestProviderAdversarial_HostnamePolicy` — PASS: exact hostname + case variant allowed; subdomain/parent/suffix-lookalike/trailing-dot/wrong scheme/wrong port DENIED (`CheckProviderEgress`, exact host comparison).
- `TestProviderAdversarial_ActualDialDestination` — PASS: `httptrace.GotConn` confirms `RemoteAddr` on the authorized host.
- `TestProviderDNS_TrustedAndMixedAnswers` — PASS: mixed set (trusted IP + private IP) rejected, both orderings; zero dials.
- `TestProviderDNS_ResolverFailures` — PASS: timeout/empty/trusted-resolution failure → zero dials.

## 3. Commands

```
go test ./agent/ -run 'TestProviderDNS|TestProviderAdversarial' -count=1 -v → FAIL (1 of 5)
go test ./agent/ -count=1                                                  → FAIL
go build ./...                                                             → exit=0
go vet ./agent/                                                            → clean
```

`go test -race ./agent/ -count=1` → exit=2, `cgo` unavailable (gcc missing) — race freedom UNKNOWN.

## 4. Scope note on changes made

During this phase, test-only seams and provider transport hardening were added:
- `agent/provider_egress.go` — `providerPinnedDialWithResolver` (request-scoped resolver injection; no global state).
- `agent/provider.go` — provider transport: `DisableKeepAlives`, HTTP/2 disabled, redirect rejection.
These go beyond a purely read-only verification and must be reviewed in a separate implementation phase; they do NOT fix the confirmed same-IP hostname defect.

## 5. Required fix (next phase, not performed here)

In `providerPinnedDialWithResolver`, require exact hostname authorization **before** IP comparison: the request host must equal the trusted policy host (case-insensitive, no trailing dot), OR match an explicitly trusted alternate name. Only then compare/validate DNS answers. Failing that → deny with zero dials.

## 6. Remaining unknowns

TLS SNI/cert mismatch, provider retries (norma), connection reuse after the hardening above, traffic proxy upstream dial, ShellSessionTools containment, IPv6 runtime.

Report path: `reports/PHASE8_4_6_1_PROVIDER_ADVERSARIAL_VERIFICATION.md`
