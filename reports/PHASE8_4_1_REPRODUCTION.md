# PHASE8_4_1 — REPRODUCTION (Phase A)

Defect under test: **EMPTY OR UNSEEDED SCOPE = IMPLICIT DEFAULT-ALLOW**

Reported location: `CheckScope` in `agent/scopecheck.go` (`if !seeded { return nil }`).

## 1. Source state inspected

- `agent/scopecheck.go`: `SetTaskScope`, `ClearTaskScope`, `CheckScope`, `scopeGuard`.
- `agent/httpclient.go`: `doHTTP` (single transport), `dialFn`, redirect `CheckRedirect` revalidation.
- `agent/worker.go:373-381`: scope seeding per worker execution.
- `db/task_scope.go`: `TaskScope` kinds (`company`, `root_domain`, `subdomain`, `ip`, `cidr`, `icp`, `keyword`).

Verified semantics of the `seeded` flag:

- `SetTaskScope` set `scopeSeeded = true` only when `len(entries) > 0`; an empty/nil call was a no-op, so **an unseeded scope is the normal state** (agent packages start unseeded).
- `CheckScope` returned `nil` immediately when `!seeded` — the default-allow.
- When seeded, invalid entries (unknown kind, empty domain, non-IP value, bad CIDR) were **silently skipped**: e.g. `{Kind:"ip", Value:"garbage"}` produced no match and the request fell through to `return fmt.Errorf("scope violation: ...")` — which denies the *destination*, but if another entry authorized it, the bad entry was still silently discarded (policy: "Do not silently discard invalid scope entries while accepting the remaining configuration").

A related defect was demonstrated: real auto-scope entries write `Kind:"ip"` with `Net:"10.0.0.5/32"` (see `db/task_scope.go:131-132,196-207`), while `CheckScope`'s `ip` branch read only `e.Value` — so a *valid, DB-real* ip scope did not authorize its own host.

## 2. Desired-behaviour tests written before the fix

`agent/scope_failclosed_test.go` (v1): asserts the **required** behaviour (deny) through the real code path — `CheckScope` and `doHTTP` → `dialFn`, no mocked guard.

## 3. Reproduction run (executed before any fix)

```
=== RUN   TestRepro_UnseededScopeAllows
    scope_failclosed_test.go:24: REPRODUCED P0: unseeded scope allowed an arbitrary destination
--- FAIL: TestRepro_UnseededScopeAllows (0.00s)
=== RUN   TestRepro_UnseededScopeReachesDial
    scope_failclosed_test.go:39: REPRODUCED P0: unseeded request succeeded
--- FAIL: TestRepro_UnseededScopeReachesDial (0.00s)
=== RUN   TestRepro_InvalidEntryNotIgnored
    scope_failclosed_test.go:57: REPRODUCED P0: malformed scope entry was silently ignored
--- FAIL: TestRepro_InvalidEntryNotIgnored (0.00s)
FAIL    github.com/Autumn-27/artex/agent    0.033s
exit=1
```

All three failures are consistent with the confirmed defect: unseeded requests succeed, reach `dialFn`, and invalid entries are discarded instead of rejecting the configuration.

The TCP-level reproduction is also implied: `TestRepro_UnseededScopeReachesDial` asserted that after a successful unseeded `doHTTP`, the dial counter increased (the "reaches the transport" step was asserted by the success of the request itself in this v1 test; the final suite asserts the stricter `dials == 0` for every denied case).

## 4. Findings (reproduced → fixed in same phase)

| Defect | Repro evidence | Fix |
|---|---|---|
| Unseeded scope → allow | `TestRepro_UnseededScopeAllows` / `...ReachesDial` fail | `CheckScope` returns `ErrScopeMissing` when `!seeded` |
| Empty scope → allow (n/a — previously unrepresentable) | `SetTaskScope(nil)` was a no-op, so an explicitly empty scope could not exist; a stale scope persisted | `SetTaskScope` now marks explicitly-empty as `ErrScopeEmpty`; `ClearTaskScope` makes unseeded |
| Invalid entries silently discarded | `TestRepro_InvalidEntryNotIgnored` fails | `validateScopeEntries` rejects the whole configuration with `ErrScopeMalformed` |
| Real ip scope (Net form) not matched | Code inspection + `TestCheckScope_IPNetFormAccepted` (fails pre-fix by design) | `scopeEntryIP` reads `Value` or `Net` |

## 5. Discrepancy note

The reported defect (`CheckScope` returning nil when unseeded) was reproduced exactly. The task scope flag semantics were verified by reading: the "no scope declared → allowed" comment in the old header is accurate; nothing contradicted the report.
