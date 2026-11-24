# PHASE8_4_1 — TEST RESULTS (Phase E)

## 1. Mandated commands — exact, with exit codes (re-run cleanly; `backups\phase8_4_1\test-exit-codes.txt`)

```
go test ./agent/ -count=1
  → exit=0; ok  github.com/Autumn-27/artex/agent  0.288s

go test ./agent/ -run 'Scope|NetworkBoundary|Redirect' -count=1
  → exit=0; ok  github.com/Autumn-27/artex/agent  0.049s

go test ./... -run 'Scope|NetworkBoundary|Redirect' -count=1
  → exit=0; all packages ok (see full output below)

go test ./agent/ -run 'TestGuardPanicConvertsPanicToToolError' -count=1
  → exit=0; ok  github.com/Autumn-27/artex/agent  0.038s

go build ./...          → exit=0
git diff --check        → exit=0
```

`go test ./... -run 'Scope|NetworkBoundary|Redirect'` full output:

```
ok   github.com/Autumn-27/artex/agent            0.095s
ok   github.com/Autumn-27/artex/cmd/artex        0.225s [no tests to run]
ok   github.com/Autumn-27/artex/config           0.026s [no tests to run]
ok   github.com/Autumn-27/artex/db               0.040s
?    github.com/Autumn-27/artex/enrich           [no test files]
ok   github.com/Autumn-27/artex/evidence         0.050s [no tests to run]
ok   github.com/Autumn-27/artex/guard            0.054s [no tests to run]
ok   github.com/Autumn-27/artex/intercept        0.036s [no tests to run]
ok   github.com/Autumn-27/artex/llmpool          0.027s [no tests to run]
ok   github.com/Autumn-27/artex/llmrec           0.034s
ok   github.com/Autumn-27/artex/mcphttp          0.036s [no tests to run]
ok   github.com/Autumn-27/artex/notify           0.038s
?    github.com/Autumn-27/artex/report           [no test files]
ok   github.com/Autumn-27/artex/selfupdate       0.024s [no tests to run]
ok   github.com/Autumn-27/artex/server           0.101s
ok   github.com/Autumn-27/artex/sidequestion     0.023s [no tests to run]
ok   github.com/Autumn-27/artex/traffic          0.027s [no tests to run]
```

## 2. Fail-closed suite — verbose pass list (re-run; `backups\phase8_4_1\test-results.txt`)

`go test ./agent/ -run 'TestScopeFailClosed|TestCheckScope|TestNetworkBoundaryAccounting|TestRedirectNoDial' -count=1 -v` → **PASS**, exit=0.

### New fail-closed tests (all PASS)

| Test | What it proves |
|---|---|
| `TestScopeFailClosed_Unseeded` | unseeded → `errors.Is(err, ErrScopeMissing)` |
| `TestScopeFailClosed_Empty` | explicitly empty → `ErrScopeEmpty` |
| `TestScopeFailClosed_UnseededZeroDials` | unseeded → `doHTTP` errors, dials == 0 |
| `TestScopeFailClosed_EmptyZeroDials` | empty → `doHTTP` errors, dials == 0 |
| `TestScopeFailClosed_MalformedZeroDials` | invalid ip value → `ErrScopeMalformed`, dials == 0 |
| `TestScopeFailClosed_OutOfScope` | valid scope, wrong host → `ErrScopeDenied` |
| `TestScopeFailClosed_ValidScopes` | root_domain / subdomain / ip(Value) / ip(Net) / cidr authorize |
| `TestScopeFailClosed_InvalidEntriesNotIgnored` | `[valid ip, garbage ip]` → `ErrScopeMalformed`, not allowed |
| `TestScopeFailClosed_NoStaleInheritance` | after re-seeding empty, previous scope does not authorize |
| `TestScopeFailClosed_ValidScopeDialsAuthorized` | authorized local destination → request succeeds, dials > 0 |

### Updated legacy tests (PASS)

| Test | Change |
|---|---|
| `TestCheckScope_NoScopeAllows` → `TestCheckScope_NoScopeDenies` | now asserts `ErrScopeMissing` |
| `TestCheckScope_EmptyScopeDenies` (new) | `SetTaskScope(nil)` → `ErrScopeEmpty` |
| `TestCheckScope_MalformedEntriesDeny` (new) | 10 sub-cases: company-without-id, empty domain, invalid ip value/net, empty/invalid cidr, icp w/o value, unsupported kind, mixed |
| `TestCheckScope_IPNetFormAccepted` (new) | real auto-scope form `Kind:"ip", Net:"127.0.0.1/32"` authorizes `http://127.0.0.1/x` |
| `TestNetworkBoundaryAccounting` (existing) | passes: part C's invalid-scope case returns an error containing "scope" (now `ErrScopeMalformed`); parts A/B/D unchanged and pass |
| `TestRedirectNoDial` (existing) | passes unchanged: unauthorized redirect → scope error, exactly 1 dial |
| `TestProbe*`, `TestCrawl`, `TestSessionCookieJar`, `TestLoginCookieJarFlow`, `TestIdorDiff*`, `TestFuzz`, `TestRetest`, `TestHTTPRequestTool` | updated to seed loopback scope via `seedLoopbackScope` — behaviour assertions unchanged |

## 3. Failures

- In-scope: **none** — `go test ./agent/ -count=1` → `ok`.
- Out-of-scope note: a first run of `go test ./agent/ -run 'TestScopeFailClosed|...'` exposed `TestScopeFailClosed_ValidScopeDialsAuthorized` failing because the *test* seeded `Net: srv.URL[len("http://"):]` — i.e. `127.0.0.1:61562/32` — which the new validation correctly rejects as a non-IP entry. This was a defect in the new test (a host with a port inside the ip `Net` field), fixed by seeding `hostOf(srv.URL) + "/32"`. Not a production defect.

## 4. Environmental / pre-existing issues recorded (not regressions from this phase)

- Full `go test ./...` (no DSN): `server` package fails — `skillRelPath` expects `/` separators, fails on Windows (`references\\a b.txt`); `notify_api_test.go` tests need `ARTEX_PG_DSN` or a config file; LLM retry logs show `429/500/network is unreachable`. Verified **pre-existing**: a pristine `git archive HEAD` copy of the tree produces the same failures (`backups\pristine-check\pristine.zip`).
- `TestGuardPanicConvertsPanicToToolError`: prints a recovered-panic log trace but **passes** in isolation (`exit=0`) and within the full `agent` run.
- `go vet ./agent/ ./db/ ./server/` → clean.

## 5. Raw test-run evidence

- `backups\phase8_4_1\test-results.txt` — tee'd output of the mandated commands.
- `backups\phase8_4_1\test-exit-codes.txt` — exit codes (all 0).

## 6. Skipped

- No test was skipped. No mock provider used. No live LLM inference performed in this phase (Phase VIII.3.5 covers that).
- Race detector not available in this environment (`go test -race` not run; documented in VIII.3).
