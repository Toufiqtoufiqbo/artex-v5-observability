# Phase VIII.1 â€” PHASE8_1_BASELINE: Independent Capability Audit

**Date:** 2026-11-20
**Method:** Independent source inspection (separate from Phase VIII), fact-check against
source; every claim here has file:line evidence; items marked [verify] are live-checkable.

---

## 1. Summary table (all nine audited areas)

| Area | Code verified | Tests verified | Live evidence | Remaining limitations |
|---|---|---|---|---|
| HTTPRequest + client | âœ… (auth_check, TLS, redirects, session jar) | âœ… (3 HTTPRequest tests) | âœ… (task 52/53/54/55) | no per-call request budget; no body compression |
| Probe â€” SQLi boolean | âœ… repeat 3Ã— + norm + FP suppress | âœ… (TestProbeSQLiBoolean) | âœ… (bench: 9 vs 56 vs 0) | time-based oracle unvalidated against a real blind lib |
| Probe â€” SQLi time | âœ… (SLEEP(3), threshold 2.5s) | âœ… unit tests exist, live unverified | âŒ | accuracy on other DBMS (MySQL vs Postgres vs SQLite) |
| Probe â€” XSS reflect | âœ… inert markers | âœ… | âœ… (bench: raw reflected) | no browser-lab execution (by design) |
| Probe â€” SSTI | âœ… ${7*7}/{{7*7}}/<%= 7*7 %> | âœ… | âœ… (bench: "49" in body) | only 3 template engines known |
| Probe â€” path traversal | âœ… canary signature check | âœ… | âœ… (root:x:0:0, win.ini) | signature-only; never reads real files |
| Crawl | âœ… params, classification, confidence, dedup | âœ… (TestCrawl, TestClassifyPath) | âœ… (bench hits, 24+ endpoints) | depth/rate-limited, single-threaded |
| IdorDiff | âœ… table, distinct_responses | âœ… (3 tests) | âœ… (task 54: distinct 10/11) | id enumeration cap 20; single-id does not detect |
| Login | âœ… cookie jar + token pin + verdict | âœ… (TestLoginCookieJarFlow + 3 session-jar tests) | âœ… (bench: /admin/stats 200 after SQLi login) | password appears in auth_check-free logs? (redacted) |
| Session | âœ… set/get/list/clear/auth_check | âœ… (6 tests) | âœ… live (auth_check on 3004) | sequential-only isolation (see phase 6) |
| Strategy | âœ… doc-injected into worker prompt | âœ… (structural) | âœ… (used implicitly) | LLM fidelity to strategy unverified |
| AttackPath | âœ… shared-asset/same-host/identity edges, BFS â‰¤5 | âœ… (TestAttackPath) | âœ… live (12 chains) | no vuln-fidelity of edges beyond facts |
| Retest | âœ… oracle re-run + verdict | âœ… (TestRetest) | âœ… live (3 re-verified CONFIRMED) | FIXED/REGRESSED classification pending |
| Finding Gate | âœ… reject no-artifact/solution-cite/unconfirmed | âœ… (8 tests) | âœ… (tasks 52-55) | not applied to non-finding writes |
| Hard scope floor | âœ… CheckScope + doHTTP hook + seeding | âœ… (3 tests) | âœ… live (bench scope enforced) | company fails open; fail-open on unknown kinds |

---

## 2. Module-by-module detail

### 2.1 HTTPRequest + transport (`agent/tools_http.go`, `agent/httpclient.go`)

- `httpCall` fields: Method, URL, Headers, Body, ContentType, Timeout, Insecure, FollowRedirects, NoJar (httpclient.go).
- `doHTTP`: scheme check â†’ `scopeGuard` â†’ request build â†’ session headers â†’ client.Do â†’ capture.
- Session headers from `getSessionHeaders(sessionKey(url))` then `c.Headers` (explicit override).
- Redirect default: `ErrUseLastResponse` (3xx visible).
- Cookie jar: process-wide `cookiejar.New(nil)` â€” `jarShared`.
- Session store: `sessHeaders map[string]map[string]string` with RWMutex.
- `injectQuery`, `injectForm`, `injectJSON` for probes/IdorDiff.
- **Live evidence:** tasks 53/55 â€” POST login â†’ 200 + token â†’ GET /admin/stats â†’ 200 (cookie replayed).
- **Tests:** `TestHTTPRequestTool` (POST + header), `TestHTTPRequestToolRejectsBadURL` (file/gopher/invalid), `TestSessionHeaderInjectedFromContext`, `TestSessionHeaderSkippedWhenKeyEmpty`, `TestSessionHeaderSkippedWhenNoSessionID`, `TestSessionCookieJar` (close), `TestLoginCookieJarFlow`, `TestSessionExpiry...`? (see Â§2.2).

### 2.2 Login & Session (`agent/tools_login.go`, `agent/tools_http.go`)

- `Login{url, username, password, username_param="username", password_param="password", method=POST, json, headers}` â€” Session jar + token PIN (`Authorization: Bearer <t>` if `token`/`access_token`/`jwt` in JSON).
- Verdict: `authenticated=true/false`; `looksFailed` heuristics.
- Redaction: response snippets capped at 300 chars (`snippet := string(res.Body)[:300]`); bodies capped 2MB.
- `Session` tool: set/get/list/clear/auth_check â€” no credentials in logs.
- **Live evidence:** bench-verify (PowerShell): login â†’ 200 + Set-Cookie `sid=bench-secret`; ARTEX task 55: `Login` â†’ status 200 + token snippet; `/admin/stats` 200.

### 2.3 Probes & oracles (`agent/tools_probe.go`)

- `oracleSQLiBoolean`: repeats=3, `normSet` (trim + collapse whitespace), FP-suppression rule, CONFIRMED iff some TRUE response differs from every FALSE and baseline response. Live bench: baseline=124 B / true=1861 B / false=124 B.
- `oracleSQLiTime`: `' OR SLEEP(3)-- -`, threshold=2.5s, max of 3 payload runs. Unit test exists.
- `oracleXSSReflect`: inert marker `"><svg/onload=artex<NNNNNN>`; `raw_reflected` vs `encoded`. Unit test exists.
- `oracleSSTI`: `${7*7}`, `{{7*7}}`, `<%= 7*7 %>` â†’ contains "49" vs baseline without. Unit test exists.
- `oracleTraversal`: `etc/passwd` â†’ `root:x:0:0` (Unix), `\windows\win.ini` â†’ `[fonts]`. Unit test exists.

### 2.4 Crawl (`agent/tools_probe.go`)

- BFS, same-host only, depth â‰¤ 3, pages â‰¤ 60, delay 80 ms.
- Extracts `a[href]`, `form[action/method/inputs]`, script paths (regex).
- Phase 3 extensions: `params` (page query + form inputs, dedup), `classification`, `confidence`.
- `classifyPath`: login/signin/auth/session â†’ auth; `/admin` â†’ admin; `/api`|`/rest` â†’ api; static â†’ static; search â†’ search; else public.
- `TestCrawl`, `TestClassifyPath` pass.

### 2.5 IdorDiff + Fuzz (`agent/tools_diff.go` â€” concurrent addition)

- `IdorDiff{url, param, method, id_range, params, headers}` â€” sends template for each id; returns table + `distinct_responses`.
- `Fuzz{url, param, method, class, payloads, extra}` â€” breadth fuzz, dedup by (FP, status).
- **Live:** task 54 â€” IdorDiff over /api/Users/{ID} 10-11: distinct=2 (alice, bob).

### 2.6 AttackPath (`agent/tools_attackpath.go`)

- Edges: shared asset, same host (informational), identityâ†’others.
- BFS from every node; chains â‰¤ 5; output `Chain: A -> B -> C`.
- Conservative: no auto-execute.
- **Live:** task 56 â€” 12 chains over 3 findings.

### 2.7 Retest (`agent/tools_retest.go`)

- `Retest{findings[]}` â€” re-runs oracle per finding; `verdict=CONFIRMED|NOT CONFIRMED|UNKNOWN|ERROR` + measurement lines.
- **Live:** task 56 â€” 3 findings re-verified CONFIRMED (bench app still vulnerable).

### 2.8 Finding Gate (`agent/finding_gate.go`)

- Rejects: empty evidence, solution-cite, unconfirmed-wording (unless proof), proof_type=none, vague narrative.
- `proof` field merged into evidence; `verified: true` returned in result.
- Tests: 8 cases pass.

### 2.9 Scope floor (`agent/scopecheck.go` + `httpclient.go` + `worker.go`)

- `doHTTP` calls `scopeGuard(rawURL)` â€” the hard floor.
- Seeded in `worker.execute` via `as.ListTaskScopeWithSources(taskID)`; `ClearTaskScope()` when unavailable.
- Tests: `TestCheckScope_RootDomain`, `TestCheckScope_IP`, `TestCheckScope_NoScopeAllows`.

### 2.10 Performance (`agent/` â€” `BenchableWorker`, timing in code)

- Default worker timeout `taskClockFrom(ctx)`; `runTimeout` on Worker.
- No process-level CPU/memory profiling wired. [verify-pending]

---

## 3. Concurrency & lifecycle

- Workers run as goroutines; `Worker.Execute` â†’ `execute` â†’ `agentcore` run.
- Per-intent state in `ToolSet`; `set_session_headers`/`sessionJar` are package-globals (see phase 6).
- Bench hits: `/search`, `/greet`, `/echo`, `/asset`, `/rest/user/login`, `/api/Users/*` all hit on bench app â€” 24-37 distinct endpoints per crawl.
- Timeout behavior: default 30s/120s; no retries.

---

## 4. Verification commands (reproducible)

```
cd <REPO_ROOT>
go test ./agent/ -count=1
```

All tests pass (verified live at the end of this mission).

**Remaining checks to do in later phases of the mission:** go test ./... (whole repo), redirect/DNS tests, performance numbers, retest FIXED/REGRESSED, XSS browser-lab (n/a by design).
