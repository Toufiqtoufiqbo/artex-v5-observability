# PHASE8_4_3 — EGRESS INVENTORY (Phase A)

## 1. HTTP/networking primitives

| Client | Where | Authorization today |
|---|---|---|
| `agent.doHTTP` | `agent/httpclient.go:133` | VIII.4.1 target scope (`scopeGuard`); VIII.4.3: proxy policy now `egressHTTPProxyPolicy` (direct, no env proxy); audit events emitted |
| `net/http.Client` (LLM) | `agent/provider.go:419` (`norma` module, external) | No target scope — separate provider domain (VIII.4.3 domain C); uses `http.ProxyURL` per config; untouched by this phase |
| `traffic.Traffic` proxy | `traffic/traffic.go` (go-mitmproxy v1.9.2) | VIII.4.3: explicit egress allowlist in upstream hook (Phase C) |
| `selfupdate` clients | `selfupdate/github.go`, `stage.go` | Own clients to the release host; not wired to worker execution; documented as follow-up |
| `notify` | `notify/http.go` | Existing dial-level SSRF control (`blockInternalDial`) |

## 2. ARTEX subsystems

| Subsystem | Notes | VIII.4.3 |
|---|---|---|
| Target-assessment tools (HTTPRequest, Probe, IdorDiff, Fuzz, Crawl, Login) | via `doHTTP` | VIII.4.1 + VIII.4.3 (audit, proxy policy) |
| LLM provider clients | `agent/provider.go` (norma) | domain B, untouched |
| Traffic proxy (8788) | `traffic.Traffic` | VIII.4.3 allowlist (Phase C) |
| Notifications (dingtalk/email/webhook) | own clients | follow-up (documented) |
| Self-update | `selfupdate` | follow-up (documented) |
| Background workers | norma `agentcore` session workers | out of this phase's mandate (VIII.4.2 contained worker tools) |

## 3. Tool execution

| Mechanism | State | VIII.4.3 |
|---|---|---|
| Bash (SDK) | VIII.4.2 guard (13-token heuristic) | Contained; per-process isolation follow-up |
| `exec.Command`/`CommandContext` | none in this tree (grep: no direct use outside norma) | — |
| WebFetch | VIII.4.2 disabled in worker | — |
| WebSearch | VIII.4.2 disabled in worker | — |
| MCP tools | VIII.4.2 `dropMCPTools` for worker; `ToolResolve` path | — |
| Server enrichment (`enrich`) | own `http.Client` (`enrich/enrich.go:94`), `InsecureSkipVerify: true`, DNS + HTTP probes of discovered assets | VIII.4.3: disabled? **No — documented as disabled via flag follow-up** |
| Python/Node subprocess | Bash guard covers shell; inline network in `python -c`/`node -e` not covered | heuristic limitation documented |

## 4. Notes on enrichment

`enrich/enrich.go` `buildClient`:
- Dedicated `http.Client`, `InsecureSkipVerify: true` (documented: pentest probe).
- Proxy via `proxy()` callback (the traffic proxy when configured).
- `doDNS` (ResolveDomain) and `doHTTP` (ProbeSite) — unauthenticated, no task scope.
- Jobs enqueue from `insertAssets` (server-side, not workers).

**Decision (Phase F/H):** enrichment egress remains a documented follow-up. The phase's `Enrichment` limitation row (see `PHASE8_4_3_TRAFFIC_PROXY.md` §6 and `PHASE8_4_3_FINAL.md`) records that it was investigated and is not claimed as controlled. Disabling enrichment for the new binary would have required a new configuration flag and behavior change beyond this phase's mandate; recorded as `PARTIAL — RUNTIME NOT VERIFIED` per acceptance matrix.

**Follow-up (not this phase):** enrichment network destinations validated against task scope or disabled.

## 5. Notes on Bash heuristic limits (unchanged)

- `nc` excluded (false-positive risk in scripts).
- Inline network in `python -c` / `node -e` one-liners not detected.
- `cmd`/`powershell` inline network tokens (e.g., `iwr` alias variants) partial coverage.
- Documented in `reports/PHASE8_4_2_BYPASS_CONTAINATION.md`.

## 6. Sources inspected

- `agent/httpclient.go` (doHTTP, dialFn, redirect guard)
- `agent/provider.go:419`
- `traffic/traffic.go` (proxy, upstream hook, `SetUpstreamProxy`, `ProxyAddr`)
- `traffic/egress.go` (new), `traffic/egress_test.go` (new)
- `agent/worker.go` (VIII.4.1 + VIII.4.2 + VIII.4.3 changes)
- `agent/bash_guard.go` (VIII.4.2)
- `agent/scopecheck.go` (VIII.4.1 + VIII.4.3 audit additions)
- `agent/egress_policy.go` (new)
- `enrich/enrich.go`, `selfupdate/*`, `notify/http.go`
- norma `tool/bash.go`, `permission` (external, read-only)
