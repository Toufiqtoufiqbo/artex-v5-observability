# ARTEX PROJECT ATLAS — partial (budget-limited)

**Verdict: `PARTIAL — BUDGET EXHAUSTED`** — sections not marked otherwise are based on this session's executed commands; unverified items are labeled.

## 1. Executive summary
ARTEX is a Go multi-agent autonomous pentest platform (HTTP server + PostgreSQL + Next.js UI) that plans security tasks via an LLM planner and executes tools through workers. Modified v5 = upstream 0.3.15 + additive measurement/scope tooling. Baseline archive identity unverified.

## 2. Identity
- Modified: `artex-original` @ `e6ec569` (detached), dirty tree (SOURCE VERIFIED via `git status`).
- Baseline: `artex-global-main.zip` SHA-256 `93AFBEC0…CD11E2`, 8,944,269 B; no git metadata → commit UNPROVEN.
- Go 1.26.11; `norma` v0.4.3 (modified) vs v0.4.4 (baseline) (SOURCE VERIFIED).

## 3. Structure (SOURCE VERIFIED)
`agent/` (planner, worker, tools, graphs), `server/` (API, manager, llmpool), `db/`, `traffic/`, `evidence/`, `guard/`, `intercept/`, `notify/`, `selfupdate/`, `cmd/artex`, `web/` + `server/webui/dist` (generated).

## 4. Architecture (SOURCE VERIFIED at module level)
HTTP server → manager/scheduler → planner/worker agents (norma) → tools (`agent/tools*.go`) → PG (asset + exploration graphs) → UI. Traffic MITM proxy on 8788.

## 5. Capability matrix (status / evidence)
| Capability | Status | Evidence |
|---|---|---|
| HTTPRequest (methods/JSON/form) | IMPLEMENTED+TESTED | `agent/tools_http.go`; `TestHTTPRequestTool` ok |
| Probe oracles (SQLi/XSS/SSTI/traversal) | IMPLEMENTED+TESTED | `agent/tools_probe.go`; 5 tests ok; live bench CONFIRMED |
| Crawl (params/classification) | IMPLEMENTED+TESTED | `TestCrawl`,`TestClassifyPath` |
| Login/Session jar/auth_check | IMPLEMENTED+TESTED | `TestLoginCookieJarFlow` etc. |
| IdorDiff/Fuzz | IMPLEMENTED+TESTED | `agent/tools_diff.go`; tests ok |
| AttackPath/Retest | IMPLEMENTED+TESTED | unit tests ok; live 12 chains; 3 re-verified |
| Finding Gate | IMPLEMENTED+TESTED | 8 tests; live rejections observed |
| Scope floor + dial accounting | IMPLEMENTED+TESTED | `TestCheckScope_*`; `TestNetworkBoundaryAccounting` (0 dials on reject) |
| Redirect revalidation | IMPLEMENTED+TESTED | `TestRedirectNoDial` |
| LLM provider path observability | UNKNOWN | no dispatch/response events found in logs; `llm/records` total=0 |
| DNS/actual-destination pinning | NOT IMPLEMENTED | audit |
| Retry authorization | NOT IMPLEMENTED | audit |
| Concurrent scope isolation | UNTESTED | `go test -race` unavailable (no gcc) |
| Multi-user session isolation | PARTIALLY IMPLEMENTED | sequential jar only |

## 6. Security (SOURCE VERIFIED)
Scope: `scopecheck.go` (company fail-closed; root_domain/subdomain/ip/cidr); hook in `doHTTP`; dial-level `dialFn` accounting proves 0 connections on reject. Redirect: `CheckRedirect` revalidates + strips credentials cross-origin. Finding Gate rejects solution-cites/unconfirmed. Limits: empty-scope still allows; proxy `ProxyFromEnvironment` inherited (UNVERIFIED policy); DNS not pinned.

## 7. Data/API/frontend
PG-backed tasks/assets/exploration/finding/evidence; API routes verified (`/api/auth/login`, `/api/tasks`, `/api/llm*`, `/api/exploration/*`). Frontend: Next.js static export; NOT INVESTIGATED in depth.

## 8. Testing (RUNTIME VERIFIED)
`go test ./agent/` ok (0.278s); baseline ok (0.078s). Full `go test ./...` has pre-existing/env failures (DSN; Windows perms) — documented earlier, one confirmed pre-existing via stash.

## 9. Baseline differential
Added: 14 `agent/*.go` + tests; edits: `tools.go`, `worker.go`, `go.mod`. Hash diff raw: ADDED 59,746 (mostly webui dist), REMOVED 197, CHANGED 574 (includes upstream drift) — SOURCE VERIFIED.

## 10. Maturity
Architecture ADEQUATE; detection reliability ADEQUATE (measured on small bench); evidence quality ADEQUATE (gate+artifacts); LLM observability WEAK; network boundary ADEQUATE (dial-proof); auth ADEQUATE; testing ADEQUATE; release readiness WEAK.

## 11. Risk register (top)
- P0: no provider-call telemetry (dispatch/response unproven) — UNKNOWN path.
- P0: empty-scope default-allow; proxy policy unverified.
- P1: DNS/actual-destination pinning absent.
- P1: retry authorization absent.
- P2: `-race` unavailable; concurrency untested.
- P3: baseline identity unverified.

## 12. Unknowns
Provider HTTP transport internals (norma), runtime model metadata, frontend depth, DB migration coverage, full-suite DSN behavior.

**Not investigated due to budget; no superiority claim.**
