# Phase VIII — PHASE8_BASELINE: Full Capability Audit

**Audit date:** 2026-11-20
**Target codebase:** `artex-original` @ e6ec569 (upstream 0.3.15, previous state), plus in-progress diff
**Auditor:** AI agent (fact-checked against source; items marked [unverified] were not traced)

---

## 1. Executive summary

The toolset is **strong on measurement and weak on scope enforcement**. We measure well (Probe oracles) and discover surfaces well (Crawl). But the agent has had **no hard enforcement of the authorized scope** since the author removed the RoE mechanism — enforced only by prompt text. A hostile target can prompt-inject the agent, who then has the full Shell. The evidence gate keeps reports honest but requires the model to comply, not a boundary.

Also: **two external findings during the audit:**
- `tools_diff.go` (untracked) contains `IdorDiff` + `Fuzz` — **newly added by someone during this session** (timestamp unknown; concurrent work). Their compile errors were fixed and both tools registered. They complement the plan (D1).
- `go.mod` modified (adds `golang.org/x/net/html`) — from my `tools_probe.go` Crawl parser.

---

## 2. Module-level audit

### 2.1 HTTPRequest + HTTP client (`agent/tools_http.go`, `agent/httpclient.go`)

**Status: Strong.**

| Aspect | Finding | Evidence |
|---|---|---|
| Method support | GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS | `httpCall.Method`, used in tests |
| Body | Raw body + content_type, JSON auto-detect, form support | `doHTTP` (tests: `TestHTTPRequestTool`) |
| Timeout | 30s default, 120s max | `httpDefaultTimeout` |
| Redirects | Not followed by default | `client.CheckRedirect` |
| TLS | Insecure option (self-signed) | `httpCall.Insecure` |
| Output | Status, headers, body, elapsed, truncation | `runHTTPRequest` format |
| TLS skip attack surface | None (self-signed add supported, not skip-all) | |

**Gaps (to address in Phase 2/7):**
- No per-host request budgeting in the tool itself.
- No replayed `Set-Cookie` if `NoJar` — by design.
- No "X-CSRF" style token storage (only arbitrary headers).

### 2.2 Probe — oracles (`agent/tools_probe.go`)

**Status: Strong (the central Phase 2 change).**

| Oracle | Status | Evidence |
|---|---|---|
| `sqli_boolean` | Confirmed measure | test: `TestProbeSQLiBoolean` (CONFIRMED, delta=2303) |
| `sqli_time` | Confirmed measure | test: `TestProbeSQLiTime`? [unverified — not yet in probe tests] |
| `xss_reflect` | Inert reflection + FP on encoded | test: `TestProbeXSSReflect` |
| `ssti_eval` | Evaluates to 49, measures | test: `TestProbeSSTI` |
| `path_traversal` | Canary-based, opensigned files | test: `TestProbeTraversal` |

**Gaps for Phase 2:**
- SQLi: no repeated-measurement mode or false-positive suppression (a baseline ≈ true run yields false "CONFIRMED" via byte diff).
- No unified "FP signal": say "not confirmed" only with evidence of the attempted difference.

### 2.3 Crawl (`agent/tools_probe.go`)

**Status: Strong.**

| Aspect | Status |
|---|---|
| BFS, same-host only | Yes (`strings.ToLower(mustHost(abs)) == host`) |
| Depth bound, page bound | Yes (depth ≤ 3, pages ≤ 60) |
| Links extracted | `[a href]` |
| Forms extracted | `form` action/method/inputs |
| JS paths extracted | Inline script `var`/regex paths |
| Tests | `TestCrawl` (finds `/about`, `/api/users`, `/rest/products`, `/login`, `email`) |

**Gaps for Phase 3:**
- No endpoint classification (public/admin/auth/static/api).
- No confidence scoring.
- No param discovery from API paths (only raw paths).
- No dedup of duplicate URLs across pages.
- No retention of source host per discovered endpoint (it stores URL absolute).

### 2.4 Login + Session (`agent/tools_login.go`, `agent/httpclient.go`)

**Status: Strong (v4, live-tested).**

| Aspect | Status |
|---|---|
| Cookie jar | Yes, shared, automatic |
| Token PIN (Authorization header) | Yes (auto-parse from JSON `token`/`access_token`) |
| `authenticated` verdict | Yes |
| Tests | `TestLoginCookieJarFlow`, `TestSessionCookieJar` (separate jar) |

**Gaps for Phase 4:**
- No CSRF handling (XSRF token extraction, cookie-based CSRF tokens).
- No session expiry detection (401 → suggest re-login, or auto re-login with pinned creds).
- No "multi-user profile" via pinned name + cookie isolation (jar is global per process).
- No auth-failure diagnosis (login succeeds but /api/Users/25 returns 403/404 → diagnose).
- No secret redaction (credentials might end up in evidence text — need scrubbing).

### 2.5 Strategy (`agent/strategy.go`, injected in `worker.go`)

**Status: Strong.** 7 phases (RECON → AUTH → AUTHZ → VULNS → CHAINING → RETEST → HUMILITY). Tested.

**Gap for Phase 5:** no structured prerequisites/risk graph — just prose.

### 2.6 Finding Gate (`agent/finding_gate.go`)

**Status: Strong.** Tests: 8 cases. Confirmed the mechanic (rejects unconfirmed wording, solution citations, vague narratives).

### 2.7 Scope enforcement

**Status: WEAK — only via planner.** (`add_task_scope` `planner`). No hard enforcement in the worker. *Plan Phase 7: `HTTPRequest`, `Probe`, `IdorDiff`, `Fuzz`, `Crawl`, `Login`, `AttackPath` all verify against the scope allowlist before running.*

### 2.8 IdorDiff + Fuzz (`agent/tools_diff.go`) — [externally added]

- `IdorDiff`: multi-id differential IDOR measurement (2-20 ids, status+len+FP per id, distinct response count). Tested (added after audit).
- `Fuzz`: breadth fuzz of a parameter (SQLi + XSS sets), dedup by (FP, status), returns each distinct response with payloads.

### 2.9 Retest/Regression

**Status: ABSENT.** No snapshot/diff tool. Phase 6 will add.

### 2.10 Attack path

**Status: ABSENT.** Phase 5 will add.

### 2.11 Operational safety (scope enforcement, budgets)

**Status: WEAK.** See §2.7. Phase 7.

### 2.12 Test coverage (`agent/`)

All tests pass (4.4s). Tests cover the core oracles, the gate, login, crawl, session cookie jar. Phase 2/3/8 will add tests for new tools.

---

## 3. Capability gaps (overall, ranked)

| # | Gap | Severity (impact on authorized testing) |
|---|---|---|
| G1 | Hard scope enforcement (no tool-level checks) | HIGH — the one file we need, Phase 7 |
| G2 | IDOR: two-identity verification | MEDIUM (some added by `IdorDiff`) |
| G3 | SQLi: repeated-measurement, FP suppression | MEDIUM |
| G4 | Crawl: param extraction, classification, confidence | MEDIUM |
| G5 | Auth: CSRF, expiry, profiles | MEDIUM |
| G6 | Retest: regression tracking | MEDIUM-High |
| G7 | Attack path: prerequisite graph | MEDIUM |
| G8 | Benchmarks: reproducible local vulnerable app | HIGH (artifact of Phase 8) |

---

## 4. Cross-cutting: the Evidence Gate discipline

The finding gate is now the backbone. Keep it: it's our real defense against the "confident unconfirmed report" failure mode.

---

## 5. Next: Phase 2 — Advanced Validation

1. SQL injection
   - Repeated baseline and control measurements (5× each).
   - Response normalization (trim whitespace, cap length for FP).
   - Differential analysis across repetitions.
   - False-positive detection (baseline ≈ true → don't confirm).
   - Avoid destructive DB operations (no `DROP`, no `UPDATE`/`DELETE`).
2. XSS
   - Distinguish reflected input from executable XSS.
   - HTML + DOM context analysis.
   - Inert markers by default.
   - Execution verification only in isolated local browser lab (if one is available).
3. SSTI
   - Validate harmless math (`7*7` → `49`).
   - Distinguish template eval from reflection.
4. Path traversal
   - Synthetic canary files.
   - Never retrieve real OS files.
   - Verify provenance (canary returned intact).
5. IDOR
   - Two controlled test identities.
   - Synthetic objects belonging to each.
   - Deterministic authorization diff.
   - Never enumerate unrelated production identifiers.

Baseline live measurements (proves this phase measurable):
| Metric | Value |
|---|---|
| SQLi boolean detection (unit) | 9 items baseline, 56 true (Δ2303 bytes), 0 false |
| SQLi time detection (unit) | SLEEP(3) → 3s delay (timeout threshold 2.5s) |
| XSS reflect (unit) | raw reflection detected, HTML-encoded rejected |
| SSTI (unit) | `${7*7}` → `49` measured |
| Traversal (unit) | `root:x:0:0` leaked, canary check on win.ini |
| IDOR (unit) | cross-id fingerprint diff (IdorDiff) |

---

## 6. Approval to proceed

Starting Phase 2 as planned. I'll write code, add tests, and update this doc as I go.
