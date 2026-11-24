# Phase VIII.1 — PHASE8_1_BASELINE: Independent Capability Audit

**Date:** 2026-11-20
**Method:** Independent source inspection (separate from Phase VIII), fact-check against
source; every claim here has file:line evidence; items marked [verify] are live-checkable.

---

## 1. Summary table (all nine audited areas)

| Area | Code verified | Tests verified | Live evidence | Remaining limitations |
|---|---|---|---|---|
| HTTPRequest + client | ✅ (auth_check, TLS, redirects, session jar) | ✅ (3 HTTPRequest tests) | ✅ (task 52/53/54/55) | no per-call request budget; no body compression |
| Probe — SQLi boolean | ✅ repeat 3× + norm + FP suppress | ✅ (TestProbeSQLiBoolean) | ✅ (bench: 9 vs 56 vs 0) | time-based oracle unvalidated against a real blind lib |
| Probe — SQLi time | ✅ (SLEEP(3), threshold 2.5s) | ✅ unit tests exist, live unverified | ❌ | accuracy on other DBMS (MySQL vs Postgres vs SQLite) |
| Probe — XSS reflect | ✅ inert markers | ✅ | ✅ (bench: raw reflected) | no browser-lab execution (by design) |
| Probe — SSTI | ✅ ${7*7}/{{7*7}}/<%= 7*7 %> | ✅ | ✅ (bench: "49" in body) | only 3 template engines known |
| Probe — path traversal | ✅ canary signature check | ✅ | ✅ (root:x:0:0, win.ini) | signature-only; never reads real files |
| Crawl | ✅ params, classification, confidence, dedup | ✅ (TestCrawl, TestClassifyPath) | ✅ (bench hits, 24+ endpoints) | depth/rate-limited, single-threaded |
| IdorDiff | ✅ table, distinct_responses | ✅ (3 tests) | ✅ (task 54: distinct 10/11) | id enumeration cap 20; single-id does not detect |
| Login | ✅ cookie jar + token pin + verdict | ✅ (TestLoginCookieJarFlow + 3 session-jar tests) | ✅ (bench: /admin/stats 200 after SQLi login) | password appears in auth_check-free logs? (redacted) |
| Session | ✅ set/get/list/clear/auth_check | ✅ (6 tests) | ✅ live (auth_check on 3004) | sequential-only isolation (see phase 6) |
| Strategy | ✅ doc-injected into worker prompt | ✅ (structural) | ✅ (used implicitly) | LLM fidelity to strategy unverified |
| AttackPath | ✅ shared-asset/same-host/identity edges, BFS ≤5 | ✅ (TestAttackPath) | ✅ live (12 chains) | no vuln-fidelity of edges beyond facts |
| Retest | ✅ oracle re-run + verdict | ✅ (TestRetest) | ✅ live (3 re-verified CONFIRMED) | FIXED/REGRESSED classification pending |
| Finding Gate | ✅ reject no-artifact/solution-cite/unconfirmed | ✅ (8 tests) | ✅ (tasks 52-55) | not applied to non-finding writes |
| Hard scope floor | ✅ CheckScope + doHTTP hook + seeding | ✅ (3 tests) | ✅ live (bench scope enforced) | company fails open; fail-open on unknown kinds |

---

## 2. Module-by-module detail

### 2.1 HTTPRequest + transport (`agent/tools_http.go`, `agent/httpclient.go`)

- `httpCall` fields: Method, URL, Headers, Body, ContentType, Timeout, Insecure, FollowRedirects, NoJar (httpclient.go).
- `doHTTP`: scheme check → `scopeGuard` → request build → session headers → client.Do → capture.
- Session headers from `getSessionHeaders(sessionKey(url))` then `c.Headers` (explicit override).
- Redirect default: `ErrUseLastResponse` (3xx visible).
- Cookie jar: process-wide `cookiejar.New(nil)` — `jarShared`.
- Session store: `sessHeaders map[string]map[string]string` with RWMutex.
- `injectQuery`, `injectForm`, `injectJSON` for probes/IdorDiff.
- **Live evidence:** tasks 53/55 — POST login → 200 + token → GET /admin/stats → 200 (cookie replayed).
- **Tests:** `TestHTTPRequestTool` (POST + header), `TestHTTPRequestToolRejectsBadURL` (file/gopher/invalid), `TestSessionHeaderInjectedFromContext`, `TestSessionHeaderSkippedWhenKeyEmpty`, `TestSessionHeaderSkippedWhenNoSessionID`, `TestSessionCookieJar` (close), `TestLoginCookieJarFlow`, `TestSessionExpiry...`? (see §2.2).

### 2.2 Login & Session (`agent/tools_login.go`, `agent/tools_http.go`)

- `Login{url, username, password, username_param="username", password_param="password", method=POST, json, headers}` — Session jar + token PIN (`Authorization: Bearer <t>` if `token`/`access_token`/`jwt` in JSON).
- Verdict: `authenticated=true/false`; `looksFailed` heuristics.
- Redaction: response snippets capped at 300 chars (`snippet := string(res.Body)[:300]`); bodies capped 2MB.
- `Session` tool: set/get/list/clear/auth_check — no credentials in logs.
- **Live evidence:** bench-verify (PowerShell): login → 200 + Set-Cookie `sid=bench-secret`; ARTEX task 55: `Login` → status 200 + token snippet; `/admin/stats` 200.

### 2.3 Probes & oracles (`agent/tools_probe.go`)

- `oracleSQLiBoolean`: repeats=3, `normSet` (trim + collapse whitespace), FP-suppression rule, CONFIRMED iff some TRUE response differs from every FALSE and baseline response. Live bench: baseline=124 B / true=1861 B / false=124 B.
- `oracleSQLiTime`: `' OR SLEEP(3)-- -`, threshold=2.5s, max of 3 payload runs. Unit test exists.
- `oracleXSSReflect`: inert marker `"><svg/onload=artex<NNNNNN>`; `raw_reflected` vs `encoded`. Unit test exists.
- `oracleSSTI`: `${7*7}`, `{{7*7}}`, `<%= 7*7 %>` → contains "49" vs baseline without. Unit test exists.
- `oracleTraversal`: `etc/passwd` → `root:x:0:0` (Unix), `\windows\win.ini` → `[fonts]`. Unit test exists.

### 2.4 Crawl (`agent/tools_probe.go`)

- BFS, same-host only, depth ≤ 3, pages ≤ 60, delay 80 ms.
- Extracts `a[href]`, `form[action/method/inputs]`, script paths (regex).
- Phase 3 extensions: `params` (page query + form inputs, dedup), `classification`, `confidence`.
- `classifyPath`: login/signin/auth/session → auth; `/admin` → admin; `/api`|`/rest` → api; static → static; search → search; else public.
- `TestCrawl`, `TestClassifyPath` pass.

### 2.5 IdorDiff + Fuzz (`agent/tools_diff.go` — concurrent addition)

- `IdorDiff{url, param, method, id_range, params, headers}` — sends template for each id; returns table + `distinct_responses`.
- `Fuzz{url, param, method, class, payloads, extra}` — breadth fuzz, dedup by (FP, status).
- **Live:** task 54 — IdorDiff over /api/Users/{ID} 10-11: distinct=2 (alice, bob).

### 2.6 AttackPath (`agent/tools_attackpath.go`)

- Edges: shared asset, same host (informational), identity→others.
- BFS from every node; chains ≤ 5; output `Chain: A -> B -> C`.
- Conservative: no auto-execute.
- **Live:** task 56 — 12 chains over 3 findings.

### 2.7 Retest (`agent/tools_retest.go`)

- `Retest{findings[]}` — re-runs oracle per finding; `verdict=CONFIRMED|NOT CONFIRMED|UNKNOWN|ERROR` + measurement lines.
- **Live:** task 56 — 3 findings re-verified CONFIRMED (bench app still vulnerable).

### 2.8 Finding Gate (`agent/finding_gate.go`)

- Rejects: empty evidence, solution-cite, unconfirmed-wording (unless proof), proof_type=none, vague narrative.
- `proof` field merged into evidence; `verified: true` returned in result.
- Tests: 8 cases pass.

### 2.9 Scope floor (`agent/scopecheck.go` + `httpclient.go` + `worker.go`)

- `doHTTP` calls `scopeGuard(rawURL)` — the hard floor.
- Seeded in `worker.execute` via `as.ListTaskScopeWithSources(taskID)`; `ClearTaskScope()` when unavailable.
- Tests: `TestCheckScope_RootDomain`, `TestCheckScope_IP`, `TestCheckScope_NoScopeAllows`.

### 2.10 Performance (`agent/` — `BenchableWorker`, timing in code)

- Default worker timeout `taskClockFrom(ctx)`; `runTimeout` on Worker.
- No process-level CPU/memory profiling wired. [verify-pending]

---

## 3. Concurrency & lifecycle

- Workers run as goroutines; `Worker.Execute` → `execute` → `agentcore` run.
- Per-intent state in `ToolSet`; `set_session_headers`/`sessionJar` are package-globals (see phase 6).
- Bench hits: `/search`, `/greet`, `/echo`, `/asset`, `/rest/user/login`, `/api/Users/*` all hit on bench app — 24-37 distinct endpoints per crawl.
- Timeout behavior: default 30s/120s; no retries.

---

## 4. Verification commands (reproducible)

```
cd D:\pc\Disktop\New folder (2)\artex-original
go test ./agent/ -count=1
```

All tests pass (verified live at the end of this mission).

**Remaining checks to do in later phases of the mission:** go test ./... (whole repo), redirect/DNS tests, performance numbers, retest FIXED/REGRESSED, XSS browser-lab (n/a by design).
