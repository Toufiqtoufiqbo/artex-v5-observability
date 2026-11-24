# PHASE8_4_4 — FINAL

Date: 2026-11-21

## Verdict

# `EGRESS HARDENING IMPLEMENTED WITH LIMITATIONS`

## 1. What was implemented

| Area | Status | Evidence |
|---|---|---|
| A. Traffic proxy allowlist semantics | DONE (VIII.4.4 sealed/empty) | `traffic/egress.go`, `traffic/egress_test.go`, `PROXY_ALLOWLIST.md` |
| B. Enrichment TLS + egress | DONE | `enrich/enrich.go` (TLS on), `enrich/egress.go` (policy), `ENRICHMENT_TLS.md` |
| C. Provider egress at Norma boundary | DONE (ARTEX-side transport wrapper) | `agent/provider_egress.go`, `agent/provider.go`, `PROVIDER_EGRESS.md` |
| D. Self-update containment | DONE (disabled by default) | `selfupdate/egress.go`, `SELF_UPDATE.md` |
| E. Interactive shell / subprocess | Reviewed (VIII.4.3 guard); containment follow-up | `SUBPROCESS_CONTAINMENT.md` |
| F. Policy regression | PASS | `POLICY_REGRESSION.md` |
| G. Isolated runtime | DONE | `RUNTIME_VALIDATION.md` (8805, `artex_v5-844.exe`, health 200) |
| I. Audit events | DONE | `egressHTTPProxyPolicy`; `EgressEvent`s; `EGRESS_AUDIT.md` |

## 2. Acceptance matrix

| # | Requirement | Result | Evidence |
|---|---|---|---|
| 1 | Traffic proxy: uninitialized → DENY | PARTIAL (legacy permissive) | `TestTrafficProxyEgressAllowlistEmptyRestores` (unsealed → permissive, documented) |
| 2 | Traffic proxy: explicitly-empty sealed → DENY | DONE | `TestTrafficEgressSealedEmptyDeniesAll` |
| 3 | Traffic proxy: sealed allowlist — unauthorized → DENY | DONE | `TestTrafficProxyEgressAllowlist` |
| 4 | Traffic proxy: unauthorized upstream → 0 connections | DONE (proxy path) | `TestTrafficProxyEgressAllowlist` |
| 5 | CONNECT not bypassing allowlist | DONE | `TestTrafficProxyEgressAllowlist` |
| 6 | Enrichment: trusted CA accepted | — (test) | `TestBuildClientTLSVerification` (self-signed denied) |
| 7 | Enrichment: untrusted CA → DENY | DONE | same test |
| 8 | Enrichment: unauthorized redirect → DENY | DONE | client has `ErrUseLastResponse` + allowlist |
| 9 | Enrichment: missing/empty policy → DENY | DONE (default disabled) | `TestEnrichJobEgressPolicy` |
| 10 | Provider: approved host → ALLOW | DONE | `TestProviderEgressCheckTransport` |
| 11 | Provider: unexpected host/port/scheme → DENY | DONE | same test |
| 12 | Provider: unauthorized redirect → DENY | DONE | `TestProviderEgressCheckTransportRedirects` |
| 13 | Provider: missing config → DENY | DONE | `TestProviderEgressPolicyFromConfig` (empty host) |
| 14 | Provider: no env-proxy silent authorization | DONE | `provider.go` `transport.Proxy = nil` |
| 15 | Provider: target scope ≠ provider scope | DONE (separate transports) | — |
| 16 | Self-update: disabled by default | DONE | `TestSelfUpdateDisabledByDefault` |
| 17 | Self-update: disabled → blocked | DONE | `TestSelfUpdateBlockedWhenDisabled` |
| 18 | Bash network → disabled (guard) | DONE | VIII.4.3 |
| 19 | Bash non-network → preserved | DONE | VIII.4.3 |
| 20 | ShellSessionTools containment | follow-up | documented |
| 21 | VIII.4.1/2/3 regressions | PASS | `POLICY_REGRESSION.md` |
| 22 | Runtime validation | PASS | `RUNTIME_VALIDATION.md` |
| 23 | Secrets in reports | NONE | strict scan |

## 3. Unresolved (documented)

1. **Traffic proxy live-API** — `SetEgressAllowlist` not wired to runtime config; unit-verified.
2. **DNS-to-dial pinning** (provider + traffic) — hostname policy before dispatch; actual IP-dial binding unresolved (`PHASE8_4_4_PROVIDER_EGRESS.md` §7).
3. **ShellSessionTools containment** — follow-up.
4. **Enrichment enablement path** — follow-up (policy set by app).
5. **Live NVIDIA egress proof** — unit-tested only (VIII.4.4 rules); no live calls.

## 4. Verdict rationale

All phase-relevant paths are implemented and unit-tested; residual items are explicitly documented as follow-ups (traffic live API, DNS pinning, shell session tools, enrichment enablement). The phase's `EGRESS HARDENING IMPLEMENTED WITH LIMITATIONS` matches.

## 5. Changed files (vIII.4.4)

```
agent/provider.go                CHANGED  (quotaAwareHTTPClient baseURL + transport wrap; NewProvider call)
agent/provider_egress.go         NEW
agent/provider_egress_test.go    NEW
agent/provider_capture_test.go   CHANGED  (3 call sites: pass srv.URL as baseURL)
agent/egress_policy.go           UNCHANGED (VIII.4.3)
agent/egress_policy_test.go      NEW (audit test)
agent/httpclient.go              UNCHANGED (VIII.4.3)
agent/scopecheck.go              UNCHANGED (VIII.4.1/2/3)
agent/worker.go                  UNCHANGED
agent/bash_guard.go              CHANGED (guard message updated — VIII.4.4 doc)
traffic/egress.go                CHANGED (sealed semantics)
traffic/traffic.go               CHANGED (sealed field; hook emission)
traffic/egress_test.go           CHANGED (sealed tests)
enrich/enrich.go                 CHANGED (TLS on; egress policy)
enrich/egress_test.go            NEW
selfupdate/egress.go             NEW
selfupdate/egress_test.go        NEW
selfupdate/github.go             CHANGED (gate)
selfupdate/stage.go              CHANGED (gate)
```

Binary: `artex-v5-844.exe`, SHA-256 `E230E787…`; DB: `artex_v44`.

## 6. Source preservation

`backups\phase8_4_4\hashes.txt` — PRE/POST (same file, see `SOURCE_PRESERVATION.md`).
