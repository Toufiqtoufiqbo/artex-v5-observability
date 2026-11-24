# PHASE8_4_1 — FINAL

Date: 2026-11-21 (local, WAT/UTC+1)

## Verdict

# `FIX IMPLEMENTED WITH LIMITATIONS`

The defect was **implemented and is demonstrably fixed at the unit/integration level with zero-dial proof**, with documented limits on live worker-path coverage.

## 1. Original defect

- **Behaviour before:** `CheckScope` returned `nil` when `!seeded`; there was no "seeded but empty" state (`SetTaskScope(nil)` was a no-op). Invalid entries were skipped silently.
- **Root cause:** the authorization predicate was "seeded AND host matches" — unseeded meant "matches everything". The header even documented it: "No scope declared → allowed (existing behaviour for agents without scope)".
- **Explicitly confirmed before the fix** by `agent/scope_failclosed_test.go` v1 (reproduction run, exit=1):
  - `TestRepro_UnseededScopeAllows` — "REPRODUCED P0: unseeded scope allowed an arbitrary destination"
  - `TestRepro_UnseededScopeReachesDial` — "REPRODUCED P0: unseeded request succeeded"
  - `TestRepro_InvalidEntryNotIgnored` — "REPRODUCED P0: malformed scope entry was silently ignored"
- Secondary defect (real-world relevance): `db/task_scope.go` writes auto-scope ip entries as `Kind:"ip", Net:"10.0.0.5/32"`, but `CheckScope`'s `ip` branch read only `e.Value` → a legitimate DB ip scope did not authorize its own host.

## 2. What changed (exact)

```
agent/scopecheck.go                 CHANGED
agent/scopecheck_test.go            CHANGED
agent/scope_failclosed_test.go      NEW
agent/tools_probe_test.go           CHANGED (seed scope; assertions unchanged)
agent/tools_login_test.go           CHANGED (seed scope)
agent/finding_gate_test.go          CHANGED (seed scope)
agent/tools_diff_test.go            CHANGED (seed scope)
agent/tools_attackpath_test.go      CHANGED (seed scope)
agent/tools.go, agent/worker.go     NOT CHANGED (pre-existing v5 modifications)
```

Tracked-diff summary: `agent/tools.go +16/-2, agent/worker.go +29/-0` — both pre-existing, untouched by this phase (verified against the pre-phase backup).

Post-phase hashes: `backups\phase8_4_1\phase-change-hashes.txt`; pre-phase backup: `backups\phase8_4_1\agent-snapshot\`.

### Implementation essentials

- `SetTaskScope` now always marks the store as seeded; empty = explicit `ErrScopeEmpty`.
- `ClearTaskScope` = unseeded = `ErrScopeMissing`.
- `CheckScope`:
  1. `!seeded` → `ErrScopeMissing`
  2. `len(entries)==0` → `ErrScopeEmpty`
  3. `validateScopeEntries` → `ErrScopeMalformed` (all-or-nothing; unknown kind, empty domain, invalid ip, invalid cidr, icp/keyword without value all reject)
  4. host match → allowed; otherwise `ErrScopeDenied`
- New `scopeEntryIP` reads `Value` (bare/bracketed) or `Net` (bare IP or CIDR) — repairs the ip-Net defect.
- `scopeEntryLabel` helper; `hostIP` removed (unused).

## 3. Before / after behaviour

| Scenario | Before | After |
|---|---|---|
| Unseeded, `CheckScope("http://203.0.113.10/")` | `nil` (allowed) | `ErrScopeMissing` |
| Unseeded `doHTTP` to loopback server | succeeds (dials=1) | `ErrScopeMissing`, dials=0 |
| Seeded empty (new state) | n/a (unrepresentable) | `ErrScopeEmpty` |
| `{Kind:"ip", Value:"garbage"}` | silent-skip → other entries could allow | `ErrScopeMalformed` (config rejected) |
| `{Kind:"ip", Net:"127.0.0.1/32"}` (real auto-scope form) | NOT matched → denied | matched → allowed |
| `{Kind:"banana", Value:"x"}` | silent-skip | `ErrScopeMalformed` |
| Valid `{Kind:"ip", Value:"127.0.0.1"}` + out-of-scope host | denied | denied (unchanged) |
| Valid scope + in-scope host | allowed | allowed (unchanged) |

No `return nil` in `CheckScope` remains on an unseeded or unvalidated path — the only allow returns follow a successful scope match.

## 4. Regression tests (all executed, all passing — see Test Results)

Mandated:

```
go test ./agent/ -count=1                                  → exit=0, ok
go test ./agent/ -run 'Scope|NetworkBoundary|Redirect'      → exit=0, ok
go test ./...    -run 'Scope|NetworkBoundary|Redirect'      → exit=0 (all ok)
go build ./...                                              → exit=0
git diff --check                                            → exit=0
```

New fail-closed coverage (all PASS):

- `TestScopeFailClosed_Unseeded`, `_Empty`, `_UnseededZeroDials`, `_EmptyZeroDials`, `_MalformedZeroDials`, `_OutOfScope`, `_ValidScopes` (root_domain/subdomain/ip-value/ip-net/cidr), `_InvalidEntriesNotIgnored`, `_NoStaleInheritance`, `_ValidScopeDialsAuthorized`.
- `TestCheckScope_NoScopeDenies`, `TestCheckScope_EmptyScopeDenies`, `TestCheckScope_MalformedEntriesDeny` (10 sub-cases), `TestCheckScope_IPNetFormAccepted`.
- Network boundary (real `doHTTP` → `dialFn`): unseeded/empty/malformed → 0 dials; valid → ≥1 dial, 200.

### Dial accounting evidence (the phase asked for zero dials for rejected requests)

- `TestScopeFailClosed_UnseededZeroDials`: `dials == 0` asserted after rejected `doHTTP`.
- `TestScopeFailClosed_EmptyZeroDials`: same for empty scope.
- `TestScopeFailClosed_MalformedZeroDials`: `ErrScopeMalformed`, `dials == 0`.
- Existing `TestNetworkBoundaryAccounting` (VIII.2.2): unauthorized destination → 0 dials; invalid scope → 0 dials.

### Redirect regressions

- `TestRedirectNoDial` (existing): unauthorized redirect → scope error, exactly 1 dial (initial).
- Unchanged cross-origin credential stripping.

### Worker/task coverage

- Not live-tested at the worker level (see acceptance matrix, case 10 = `NOT VERIFIED`): `Worker.execute` still clears the scope when `ListTaskScopeWithSources` returns nothing (`worker.go:373-381`), which now yields `ErrScopeMissing`. This is code-verified but not executed against a live worker in this phase.

### Compatibility

- Health endpoints, auth endpoints, and non-network operations unaffected (`go test ./server` ok in the scoped run; health 200 verified live earlier).
- LLM provider egress not altered (`provider.go` separate client; Phase VIII.3.5 config intact).
- Detection logic, prompts, tool schemas unchanged.

## 5. Preserved limitations

1. **Not every network path**: Bash tool (curl), WebFetch, MCP, skill tools can still egress — they do not traverse `doHTTP`. Target scope does not claim to secure them.
2. **Process-global scope store**: concurrent workers on different tasks can clobber each other's scope. Separate phase (concurrency isolation).
3. **IPv6 in `hostOf`**: `[::1]:8080` parsing imperfect; scope entry forms for IPv6 need care.
4. **Validation depth**: `root_domain`/`subdomain` accept any non-empty domain string (no syntax check); no entry currently matches malformed strings, and comparison is exact/suffix, so no allow is created.
5. **Live-worker proof** is `NOT VERIFIED` (see acceptance matrix).
6. Existing full-suite failures in `server` are pre-existing/environmental (verified on pristine `git archive HEAD`).
7. The live modified-service binary was not rebuilt with this fix (implementation phase).

## 6. Remaining bypass paths (by design, not this phase)

- Bash/curl, WebFetch, MCP, skill network tools.
- Provider egress (separate domain, separately governed).
- Concurrency-safety of the scope store.
- `SetTaskScope` semantic change: callers relying on old no-op behaviour must pass a valid non-empty list.

## 7. Acceptance matrix (Phase G)

| # | Case | Result | Evidence |
|---|---|---|---|
| 1 | Missing scope | DENY | `TestScopeFailClosed_Unseeded`: `errors.Is(err, ErrScopeMissing)` — PASS |
| 2 | Nil scope | DENY | `TestScopeFailClosed_Empty` (`SetTaskScope(nil)` → `ErrScopeEmpty`); unseeded-nil → `ErrScopeMissing` — PASS |
| 3 | Empty scope | DENY | `TestScopeFailClosed_Empty`, `TestCheckScope_EmptyScopeDenies` — PASS |
| 4 | Unseeded scope | DENY | `TestScopeFailClosed_Unseeded` — PASS |
| 5 | Malformed scope | DENY | `TestCheckScope_MalformedEntriesDeny` (10 sub-cases) + `TestScopeFailClosed_MalformedZeroDials` — PASS |
| 6 | Unsupported type | DENY | `unsupported scope kind` sub-case — PASS |
| 7 | Authorized local target | ALLOW | `TestScopeFailClosed_ValidScopeDialsAuthorized` (200, dials>0); existing probe/login/diff/retest tests with seeded scope — PASS |
| 8 | Unauthorized local target | DENY | `TestScopeFailClosed_OutOfScope`, `TestNetworkBoundaryAccounting` — PASS, 0 dials |
| 9 | Unauthorized redirect | DENY | `TestRedirectNoDial` — PASS, 0 destination dials, cross-origin credentials stripped |
| 10 | Worker without scope | DENY (code) | `worker.go:373-381` ClearTaskScope on no scope; NOT VERIFIED live — the phase requires `NOT VERIFIED` |
| 11 | Health/auth non-network | Preserved | `go test ./server` ok — PASS |
| 12 | LLM provider path not treated as target request | Preserved | static: `agent/provider.go` uses its own client; `scopeGuard` not in that path — PASS (code review) |

All rows with `PASS` are backed by executed tests in `backups\phase8_4_1\test-results.txt` / `test-exit-codes.txt`.

## 8. Final answers to Phase 9 questions

1. **Empty-scope default-deny implemented and tested?** YES - `TestScopeFailClosed_Empty`, `TestScopeFailClosed_UnseededZeroDials`, `TestCheckScope_EmptyScopeDenies`.
2. **Live-worker paths verified?** NO — `NOT VERIFIED` (code-verified only; Phase VIII.4.2 can live-test).
3. **Regressions?** No — `go test ./agent/` green, all existing scope tests updated and passing, detection/prompts/schemas unchanged.

## 9. Source preservation confirmation

Per `reports/PHASE8_4_1_SOURCE_PRESERVATION.md`:

- Pre-phase state: HEAD `e6ec569…`, ` M agent/tools.go`, ` M agent/worker.go`, 19 untracked agent files (full list in `backups\phase8_4_1\git-status-short.txt`).
- Backup `backups\phase8_4_1\agent-snapshot\` (70 files) — hashes of `scopecheck.go`/`scopecheck_test.go`/`http_connection_counter_test.go` verified to match the pre-edit hashes recorded in the preservation report.
- No `git reset --hard` / `clean` / `checkout` / `stash` / dependency upgrade / destructive DB ops in this phase.
- `git diff --check` → exit 0.
- Baseline archive SHA-256 re-verified: `93AFBEC089A9D8AB00AE1DA4918DFCAF3A99AF636C1303DAE514F4EBE7CD11E2` (unchanged).
