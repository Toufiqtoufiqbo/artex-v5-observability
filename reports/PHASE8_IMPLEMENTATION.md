# Phase VIII — PHASE8_IMPLEMENTATION

**Implementation notes for the Advanced Security Validation & Attack Surface Intelligence program.**

*Modified/added files (see also `PHASE8_BASELINE.md` for the audit and `PHASE8_SECURITY_AUDIT.md` for the safety review):*

```
agent/tools_probe.go          (modified)  Probe: SQLi boolean, SQLi time, XSS reflect, SSTI, path traversal
agent/tools_http.go           (modified)  HTTPRequest + Session (cookie jar + pinned headers + auth_check)
agent/httpclient.go           (new)       Shared transport + session jar + scope guard hook
agent/scopecheck.go           (new)       Hard scope floor (Phase 7)
agent/tools_diff.go           (added, concurrent)  IdorDiff + Fuzz oracles
agent/tools_login.go          (new)       Login tool (browser path + API token path)
agent/strategy.go             (new)       Phase-aware methodology (injected into worker prompt)
agent/tools_attackpath.go     (new)       Phase 5 — AttackPath graph engine
agent/tools_retest.go         (new)       Phase 6 — Retest/regression engine
agent/tools.go, worker.go     (modified)  Tool registration, scope seeding
agent/tools_*_test.go         (new)       Unit tests for every phase
agent/bench-vuln.py           (new)       Phase 8 benchmark app (port 3004)
bench-truth.json, bench-eval.js, bench-hits.log  (new)  Phase 8 evaluation
reports/PHASE8_BASELINE.md               Audit
reports/PHASE8_IMPLEMENTATION.md         This doc
reports/PHASE8_SECURITY_AUDIT.md         Phase 7 audit
reports/PHASE8_BENCHMARK.md              Phase 8 results (pending)
```

---

## Phase 2 — Advanced Validation

### 2.1 SQL injection (`oracleSQLiBoolean`, `oracleSQLiTime`)

- **Repeated measurements**: `probeSQLiRepeats = 3` — the boolean oracle now sends baseline(1), `' OR '1'='1`, `' AND '1'='2` ×3.
- **Response normalization**: `normBody()` (trim + collapse whitespace) and `normSet()` — distinct response shapes computed over the repeat set.
- **Differential analysis**: confirmed iff some TRUE response differs from every FALSE response AND from every baseline response.
- **False-positive suppression**: if all true responses equal baseline responses (the parameter is ignored), VERDICT: NOT CONFIRMED.
- **`oracleSQLiTime`**: `' OR SLEEP(3)-- -`, threshold `probeTimeThreshold = 2.5s`, requires ≥1 payload run above threshold and ≥2.5s over baseline.
- **No destructive DB ops**: payloads are read-only (`OR`/`AND` tautologies, SLEEP; no UPDATE/DELETE/DROP).
- **Tests**: `TestProbeSQLiBoolean` (CONFIRMED, repeats=3, distinct_responses baseline=1 true=1 false=1), `TestProbeSQLiBooleanNegative`, `TestProbeSQLiTime` (SLEEP(3) → delay over baseline).

### 2.2 XSS (`oracleXSSReflect`)

- **Inert by default**: payload is `"><svg/onload=artex<NNNNNN>` — marker-based, no real alert().
- **Reflected vs. executable**: `raw_reflected` (unencoded) vs `encoded` (detected via HTML-escaping). Executable-XSS execution verification is out of scope (Phase 2 says: only in an isolated local browser lab; ARTEX has no browser lab wired).
- **Tests**: `TestProbeXSSReflect` (raw → CONFIRMED; encoded → NOT CONFIRMED).

### 2.3 SSTI (`oracleSSTI`)

- Validates `${7*7}`, `{{7*7}}`, `<%= 7*7 %>` — server response must contain `49` (unescaped) while baseline doesn't.
- **Tests**: `TestProbeSSTI`.

### 2.4 Path traversal (`oracleTraversal`)

- Canary files: `/etc/passwd` (Unix signature `root:x:0:0:root:/root:/bin/bash`) and `\windows\win.ini` (`[fonts]`).
- **Never retrieves real OS files** — only leaks the canary signature.
- **Tests**: `TestProbeTraversal` (unix_passwd confirmed).

### 2.5 IdorDiff + Fuzz (concurrent addition, `agent/tools_diff.go`)

- `IdorDiff{url, param, method, id_range, params, headers}`: sends the endpoint template for each id (default "1-6", cap 20), returns a table of (id, status, len, fingerprint, snippet) and `distinct_responses`. Used for IDOR prerequisites (Phase 5).
- `Fuzz{url, param, method, class, payloads, extra}`: breadth fuzz with built-in sets (sql_injection, xss_common), aggregates distinct (FP, status) groups with payloads that produced them.
- Fixed compile errors introduced during this session; registered both in worker toolset.
- **Tests**: `TestIdorDiff`, `TestIdorDiffSame`, `TestFuzz`.

---

## Phase 3 — Attack Surface Intelligence

`Crawl` (extended) now returns:

- `pages` — with `status` + `title`.
- `links`, `forms` (action/method/inputs), `js_paths`.
- `params` — per-endpoint parameter names extracted from: page query strings + form inputs.
- `classification` — per page: auth / admin / api / static / search / public.
- `confidence` — per page: 0–100 (status-based, login/api bonus).
- Duplicate elimination (`links` map, `params` dedup, `seen` set).
- Same-host only (hard), depth ≤ 3, pages ≤ 60.

`classifyPath`: login→auth, /admin→admin, /api|/rest→api, static files (.js/.css/.png/…)→static, /search|/query|/find→search, else public.

**Tests**: `TestCrawl` (finds /about, /api/users, /rest/products, /login, "email"), `TestClassifyPath`.

---

## Phase 4 — Authentication & Session Reliability

- **`Login{url, username, password, username_param, password_param, method, json, headers}`** (new). Two identity paths:
  - Browser path: response sets a cookie → shared jar replayed automatically by `HTTPRequest`, `Probe`, `IdorDiff`, `Fuzz`, `Crawl` (all use `doHTTP`).
  - API path: response returns `token`/`access_token`/`jwt` → pinned as `Authorization: Bearer <token>` via `setSessionHeaders`; replayed by all HTTP primitives.
  - Returns `authenticated=true/false` + raw exchange (citable evidence).
- **`Session`** (extended):
  - `set` (pin headers per host), `get`, `list`, `clear` — existing.
  - `auth_check` (new, Phase 4): sends one GET with pinned headers; returns status + snippet. For expiry detection.
- **Secret redaction**: response snippets are capped (300 chars); bodies are capped (2MB) and truncated (12k chars).
- **Credentials in logs**: `Login` output includes username (not password).
- **Tests**: `TestLoginCookieJarFlow` (cookie replayed on /account), `TestLoginRejectsMissingCreds`, `TestSessionCookieJar`, `TestSessionHeaderInjectedFromContext`, `TestSessionHeaderSkippedWhenKeyEmpty`, `TestSessionHeaderSkippedWhenNoSessionID`.

Note: multi-user test profiles are NOT a per-worker isolation feature — the jar is process-wide. Each worker gets a fresh process per intent (agentcore starts a new process), so pinning different identities is sequential. This is conservative.

---

## Phase 5 — Attack Path Intelligence

`AttackPath{findings[], max_paths}` (new):

- Nodes = findings. Edges when:
  - shared asset id;
  - same host (informational);
  - identity class (class contains auth/login/session) → connects to all findings on that host.
- BFS from every node; chains of length ≥ 2 up to depth 5.
- Output: `Chain: A (sev, class) -> B (sev, class) -> C ...`.
- Conservative: no automatic privilege escalation, no silent re-execution of chains.
- **Tests**: `TestAttackPath` (same-host chain: SQLi -> IDOR; isolated host produces no chain).

---

## Phase 6 — Retest / Regression Engine

`Retest{findings[]}` (new):

- For each finding (id/name/class/url/param/method/body_format): re-runs the same oracle (`runOracleFor`: sqli_boolean/sqli_time/xss_reflect/ssti_eval/path_traversal) and returns per-finding:
  - `retest id=... verdict=CONFIRMED|NOT CONFIRMED|UNKNOWN|ERROR` + measurement lines (VERDICT, distinct_responses, evidence).
- Mechanical verdict: CONFIRMED iff the oracle measures signal.
- **Tests**: `TestRetest`.

---

## Phase 7 — Operational Safety (Hard floor)

`scopecheck.go` (new) + hook in `doHTTP`:

- `SetTaskScope([]db.TaskScope)` / `ClearTaskScope()` — worker seeds from the task's declared scope.
- `CheckScope(rawURL)` — matches host against scope entries:
  - `root_domain`: host == domain or ends with ".domain".
  - `subdomain`: host == domain.
  - `ip`: host is the IP.
  - `cidr`: host is the IP inside the net.
  - `company`: allowed (representative of the company's assets).
- `doHTTP` refuses to send any request outside scope. Tools that wrap `doHTTP` (`HTTPRequest`, `Probe`, `IdorDiff`, `Fuzz`, `Crawl`, `Login`, `Retest`, `AttackPath` — where relevant) all inherit the floor.
- Seeding in `worker.execute`: `as.ListTaskScopeWithSources(taskID)` → `SetTaskScope(...)`; when the store is unavailable or empty, `ClearTaskScope()`.
- No scope declared → allowed (existing behaviour for agents without scope).
- **Tests**: `TestCheckScope_RootDomain`, `TestCheckScope_IP`, `TestCheckScope_NoScopeAllows`.

Honesty note: this is a host-match hard floor, not a policy engine. `root_domain` semantics include subdomains (standard industry reading). Unsupported kinds fail open.

---

## Phase 8 — Quantitative Evaluation

- **App**: `bench-vuln.py` (port 3004) — deliberate vulns:
  - SQLi (boolean): `GET /search?q=` — prod/HIT count difference.
  - XSS reflect: `GET /echo?q=` — raw reflection.
  - SSTI: `GET /greet?name=` — `${7*7}` → 49.
  - Path traversal: `GET /asset?file=` — `etc/passwd` → root:x:0:0.
  - Login SQLi + Set-Cookie: `POST /rest/user/login` (email "' OR 1=1--" → token).
  - IDOR: `GET /api/Users/{ID}` (10: alice, 11: bob).
  - Session-required admin: `GET /admin/stats` (cookie sid=bench-secret).
  - Normal endpoints (negatives): `/ok`, `GET /api/Users`.
- **Ground truth**: `bench-truth.json` (6 known vulns with verified=true).
- **Hits log**: `bench-hits.log` — per-request records (path + cookie presence) for endpoint-discovery measurement.
- **Evaluation**: `bench-eval.js <task_id>` — computes detection rate, FP rate, endpoint discovery, endpoint coverage, unmatched truth.
- **Results**: `reports/PHASE8_BENCHMARK.md` (pending — filled after the live run).

**Final acceptance gate** (per the mission): existing functionality intact (all `go test ./agent/` green), new tests pass, scope enforcement verified, findings carry reproducible evidence (gate + artifacts), FP behaviour measured (bench), no unsupported capability reported as implemented.
