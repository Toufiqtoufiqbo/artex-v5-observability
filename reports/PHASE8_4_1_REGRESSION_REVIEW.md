# PHASE8_4_1 — REGRESSION REVIEW (Phase F)

## 1. Final diff summary

```
git diff --stat (tracked)
  agent/tools.go              |  16 +++++++-
  agent/worker.go             |  29 +++++++++++++++++++
  (2 files, 43 insertions, 2 deletions — all PRE-EXISTING v5 changes, untouched by this phase)

Files changed by THIS phase (vs pre-phase backup):
  CHANGED  agent/scopecheck.go             (v5-added file; modified by this phase)
  CHANGED  agent/scopecheck_test.go         (v5-added file; modified by this phase)
  CHANGED  agent/tools_probe_test.go        (v5-added file; seed scope added)
  CHANGED  agent/tools_login_test.go       (v5-added file; seed scope added)
  CHANGED  agent/finding_gate_test.go       (v5-added file; seed scope added)
  CHANGED  agent/tools_diff_test.go         (v5-added file; seed scope added)
  CHANGED  agent/tools_attackpath_test.go   (v5-added file; seed scope added)
  NEW      agent/scope_failclosed_test.go

List: backups\phase8_4_1\phase-change-list.txt
Pre-phase hashes: backups\phase8_4_1\phase-change-hashes.txt (PRE section)
Post-phase hashes: same file (POST section, appended)
```

Why each file changed:

- `agent/scopecheck.go` — the security fix itself (fail-closed policy, validation, sentinels, `scopeEntryIP`).
- `agent/scopecheck_test.go` — `TestCheckScope_NoScopeAllows` replaced by `TestCheckScope_NoScopeDenies`; new tests for empty scope, malformed configs, IP-Net form.
- `agent/scope_failclosed_test.go` (new) — executable policy: unseeded/empty/malformed → deny with zero dials; valid scopes still authorize; no stale inheritance.
- The five `*_test.go` fixes (`tools_probe_test.go`, `tools_login_test.go`, `finding_gate_test.go`, `tools_diff_test.go`, `tools_attackpath_test.go`) — these tests performed real network calls through `doHTTP` without any scope (they relied on the old default-allow). They now call `seedLoopbackScope` to authorize the loopback test server. **No assertions were weakened; the expected tool outputs are unchanged.**

Nothing else changed: `agent/tools.go`/`agent/worker.go` diffs predate this phase (verified against the pre-phase backup hashes; this phase added no changes there).

## 2. Security controls re-verified after the fix

| Control | Status | Evidence |
|---|---|---|
| Redirect validation re-checked per hop | Active | `httpclient.go:214-229` unchanged; `TestRedirectNoDial` passes (unauthorized redirect → scope error, exactly 1 dial) |
| Cross-origin credential stripping on redirect | Active | unchanged (`Del("Authorization")`, `Del("Cookie")` on host change) |
| Dial-level connection accounting | Active | `TestNetworkBoundaryAccounting` passes; all denied requests in the new suite show 0 dials |
| Scope matching semantics for valid scopes | Preserved | `TestCheckScope_RootDomain`, `TestCheckScope_IP`, `TestCheckScope_IPNetFormAccepted`, `TestScopeFailClosed_ValidScopes/*` pass |
| Company fail-closed (VIII.2 P0) | Preserved + still tested | `TestCheckScope_CompanyFailsClosed` passes |
| Error distinction (missing/empty/malformed/denied) | New | `errors.Is` sentinels + `TestScopeFailClosed_*` category assertions |
| Provider egress (norma) unaffected | Intended | `agent/provider.go:419` uses its own `http.Client`; does not call `scopeGuard` — different authorization domain |
| Health/auth endpoints unaffected | Intended | they do not traverse `doHTTP` |
| LLM provider configuration unchanged | Intended | Phase VIII.3.5 configuration untouched; `POST /api/llm` surface untouched |

## 3. Functional compatibility review

- **Detection logic untouched**: `tools_probe.go`, `tools_http.go`, `tools_diff.go`, `tools_login.go`, `tools_retest.go`, `tools_attackpath.go` unchanged by this phase.
- **Prompts/tool schemas untouched**: no prompt or schema changes.
- **Worker scope wiring**: `worker.go:373-381` still seeds from `ListTaskScopeWithSources` when present, else `ClearTaskScope()` — now yields `ErrScopeMissing` instead of default-allow.
- **Backward-compat risk surfaced**: an agent that relied on "no scope = everything allowed" will now fail closed. This is the intended policy change; callers must declare scope (planner `add_task_scope` / `set_constraints` or the task goal's "scope strictly ..."). This is a behavioural change, not a compatibility break in the sense of breaking valid configurations.
- **New risk introduced by `SetTaskScope` semantics change**: previously `SetTaskScope(nil)` was a no-op; now it marks an explicit empty scope. Worker call sites pass `SetTaskScope(scopes)` only when `len(scopes) > 0`, so runtime behavior is unchanged; only direct callers are affected.

## 4. Strengthened paths (bonus, not part of the fix)

- `scopeEntryIP` now accepts the real `Kind:"ip", Net:"a.b.c.d/32"` form produced by `db/task_scope.go` — a demonstrated defect is repaired, so auto-scoped IPs now authorize.
- `ErrScopeMissing/Empty/Malformed/Denied` sentinels allow precise caller handling.

## 5. Not-in-scope / remaining bypass paths (documented)

1. **Bash tool (curl), WebFetch, MCP, skill tools** can still perform arbitrary network egress in the worker context — they do not traverse `doHTTP`. Target-scope fail-closed does not claim to secure these paths.
2. **Provider egress** is separately governed by the norma client; a valid target scope does not authorize LLM calls and vice versa.
3. **Process-global scope store** (`scopeEntries` + `scopeSeeded`): `Worker.execute` seeds/clears per worker run, but the store is process-global, so concurrent workers on different tasks can clobber each other's scope. Out of scope: full concurrency isolation (listed as a separate phase).
4. **IPv6 host parsing in `hostOf`** (`[::1]:8080` → LastIndex(":") yields `[::1]`) — scope entries for IPv6 destinations need careful entry forms; not covered by this phase.
5. **Validation gap**: `root_domain`/`subdomain` entries with syntactically odd domains (e.g. `..`) are accepted as non-empty. Not reachable as an authorization increase at this layer because host comparison is exact-match/suffix, and no entry currently matches `..`.
6. **`icp`/`keyword` scopes** never authorize hosts by design — a scope of only these denies (correct fail-closed).

## 6. Requirement checklist (Phase F style)

| # | Requirement | Status |
|---|---|---|
| 1 | Existing scope matching rules not weakened | ✓ verified via existing tests |
| 2 | No default-allow introduced elsewhere | ✓ verified: `CheckScope` has no `return nil` on an unseeded/unvalidated path; `doHTTP`/`scopeGuard` only return the deny errors |
| 3 | Redirect validation active | ✓ |
| 4 | Dial accounting active | ✓ |
| 5 | Error propagation correct | ✓ errors propagate from `doHTTP` to tool callers |
| 6 | Valid explicit scopes remain compatible | ✓ |
| 7 | Non-network endpoints not accidentally blocked | ✓ provider/health verified in Phase F; `go test ./... -run 'Scope|NetworkBoundary|Redirect'` green |
| 8 | LLM provider configuration unchanged | ✓ |
| 9 | Detection logic unchanged | ✓ |
| 10 | Planner/Worker prompts unchanged | ✓ |
| 11 | Existing uncommitted changes preserved | ✓ (`tools.go`, `worker.go` untouched; backup verified) |
| 12 | No secrets added to source or reports | ✓ (verified) |

## 7. Post-phase hash continuity

`backups\phase8_4_1\phase-change-hashes.txt`: pre (before edit) and post (after) hashes recorded; PRE scopecheck.go hash `5AA0E858…` is reproduced in `backups\phase8_4_1\agent-snapshot\scopecheck.go` — proving the backup predates the fix.

## 8. Conclusion of Phase F

The diff is minimal (`agent/` only), detection and prompts untouched, all security controls verified active, and the behavioural change is the requested fail-closed hardening. Bypass paths remain documented.

## 9. Regression verdict for Phase F

`PASS` — no regressions in valid authorization semantics; the only functional change is the intended fail-closed policy.
