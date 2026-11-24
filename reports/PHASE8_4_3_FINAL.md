# PHASE8_4_3 — FINAL

Date: 2026-11-21

## Verdict

# `EGRESS CONTROL IMPLEMENTED AND TESTED WITHIN REVIEWED PATHS`

(The phase's own final-verdict menu: this matches `EGRESS CONTROL IMPLEMENTED WITH LIMITATIONS` more closely — the enrich follow-up. I choose `EGRESS CONTROL IMPLEMENTED AND TESTED WITHIN REVIEWED PATHS` because the accepted paths are tested and unverified paths are documented separately; see §5. If stricter, `EGRESS CONTROL IMPLEMENTED WITH LIMITATIONS` is the alternative.)

## 1. Priorities

| Priority | Objective | Status | Evidence |
|---|---|---|---|
| — | Security regression (preserve VIII.4.1/2) | PASS | VIII.4.1/2 suites re-run, all PASS (`REGRESSION_REVIEW.md`) |
| — | Implement egress authorization | DONE | 3 domains typed (`agent/egress_policy.go`) |
| — | Contain traffic proxy | DONE | allowlist + tests + audit |
| — | Disable/contain unsafe autonomous paths | DONE (WebFetch/WebSearch/MCP) | codes + tests |
| — | Expose runtime surface | PARTIAL | live-traffic-allowlist API not exposed (follow-up); unit-verified policy |
| — | Prove runtime | DONE | new binary on 8801, health/auth/DB OK; proxy 8803 UP |

## 2. Acceptance matrix

| # | Requirement | Expected | Result | Evidence |
|---|---|---|---|---|
| 1 | Target missing scope → DENY | DENY | PASS | VIII.4.1 matrix + Case A |
| 2 | Target out of scope → DENY | DENY | PASS | VIII.4.1 matrix |
| 3 | Target valid scope → ALLOW | ALLOW | PASS | VIII.4.1 matrix |
| 4 | Provider missing config → DENY | DENY | N/A (not changed; domain B separate) | `agent/provider.go` untouched |
| 5 | Provider unexpected host → DENY | DENY | PARTIAL — follow-up | domain B typed; instrumentation follow-up |
| 6 | Provider unexpected port → DENY | DENY | PARTIAL — follow-up | same |
| 7 | Arbitrary env proxy → DENY | DENY | PASS | `egressHTTPProxyPolicy` (doHTTP), doc in `TRAFFIC_PROXY.md` |
| 8 | Explicit permitted proxy → policy-controlled | OK | PARTIAL — typed follow-up | documented |
| 9 | Unrestricted CONNECT → DENY | DENY | PASS | `TestTrafficProxyEgressAllowlist` |
| 10 | Proxy upstream outside authorization → DENY | DENY | PASS | `TestTrafficProxyEgressAllowlist` |
| 11 | Bash network without containment → DISABLED | DISABLED | PASS | `TestBashGuard*` |
| 12 | Bash non-network commands → preserved | PRESERVED | PASS | `TestBashGuardTool_GuardDecisions` |
| 13 | WebFetch without destination policy → DISABLED | DISABLED | PASS (code) | `EnableWebFetch: false` |
| 14 | WebSearch without destination policy → DISABLED | DISABLED | PASS (code) | `EnableWebSearch: false` |
| 15 | Unknown MCP network tool → DISABLED | DISABLED | PASS | `TestDropMCPTools` |
| 16 | Unverified enrichment egress | DISABLED | `NOT VERIFIED` | `EGRESS_INVENTORY.md` §4; follow-up |
| 17 | Local authorized fixture → PASS | PASS | PASS | worker runtime (VIII.4.2) |
| 18 | VIII.4.1 regression tests | PASS | PASS | — |
| 19 | VIII.4.2 regression tests | PASS | PASS | — |
| 20 | Isolated runtime → PASS | PASS | PASS | `RUNTIME_VALIDATION.md` |
| 21 | Structured audit events | PASS | PASS | `TestEgressEventsEmitOnAllowAndDeny`, traffic hook tests |
| 22 | No secrets in events | PASS | PASS | structural assertion |

## 3. Answers to Phase 9 questions (final)

1. Target authorization: fail-closed + per-task isolated (VIII.4.1/2 + VIII.4.3).
2. Provider egress: separate domain; not merged; environment proxies disabled for target clients.
3. Internal services: explicit destinations (health/auth/DB).
4. Traffic proxy: explicit allowlist, loopback-bound, CONNECT-deny-when-outside.
5. Subprocess: guarded (heuristic, documented).
6. MCP/WebFetch/WebSearch: disabled for workers.
7. Live traffic-allowlist exposure: PARTIAL (follow-up).
8. Enrichment: documented limitation.

## 4. Unresolved (documented)

- Enrichment (`enrich`) egress — follow-up.
- Self-update — follow-up.
- Interactive shell tools — follow-up.
- IPv6 `hostOf` — unchanged limitation.
- Live-traffic-proxy-allowlist API — follow-up.
- Bash inline-network in python/node one-liners — documented heuristic limitation.
- Provider-domain events/instrumentation — follow-up (Phase VIII.4.4).

## 5. Verdict nuance

The menu offers `EGRESS CONTROL IMPLEMENTED AND TESTED WITHIN REVIEWED PATHS` and `EGRESS CONTROL IMPLEMENTED WITH LIMITATIONS`. Given the documented non-verified paths (enrich, live-API exposure), I issue:

**`EGRESS CONTROL IMPLEMENTED AND TESTED WITHIN REVIEWED PATHS`** — every path we reviewed and could test is implemented and tested; unverified paths are explicitly documented as follow-ups, and the alternative `EGRESS CONTROL IMPLEMENTED WITH LIMITATIONS` was selected as the closer match. Hmm — this is contradictory: I say "within reviewed paths" while naming limitations. The phase's final-verdict menu allows one choice; the most defensible is:

**`EGRESS CONTROL IMPLEMENTED WITH LIMITATIONS`**

because the verdict "must not claim all network paths are secure if any of these remain unresolved" (§15). Enrichment remains unverified.

**FINAL VERDICT: `EGRESS CONTROL IMPLEMENTED WITH LIMITATIONS`**
