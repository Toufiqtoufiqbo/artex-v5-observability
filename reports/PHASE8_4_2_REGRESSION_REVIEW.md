# PHASE8_4_2 — REGRESSION REVIEW (Phase J)

## 1. Diff summary

```
Changed files by this phase (backup diff):
  CHANGED  agent/scopecheck.go                  (task-scoped store, VIII.4.2 API, CheckScopeForKey)
  CHANGED  agent/httpclient.go                  (scopeGuard(ctx, url) × 2)
  CHANGED  agent/worker.go                      (per-task seed/clear + defer clear; Bash guard; WebFetch/WebSearch off; MCP drop)
  NEW      agent/bash_guard.go
  NEW      agent/bash_guard_test.go
  NEW      agent/scope_task_isolation_test.go

Untouched (pre-existing, not modified by this phase):
  agent/tools.go, agent/worker.go diffs predate this phase (verified vs backup)
  agent/scopecheck.go VIII.4.1 changes preserved (see hashes)

Pre-phase hashes:  backups\phase8_4_2\phase-change-hashes.txt (PRE section)
Post-phase hashes: backups\phase8_4_2\phase-change-hashes.txt (appended POST section)
```

## 2. VIII.4.1 guarantees re-verified (mandated Phase E commands, executed)

```
go test ./agent/ -count=1                                   → exit=0; ok github.com/Autumn-27/artex/agent
go test ./agent/ -run 'Scope|NetworkBoundary|Redirect'      → exit=0; ok github.com/Autumn-27/artex/agent
go test ./...    -run 'Scope|NetworkBoundary|Redirect'      → exit=0 (all packages ok)
go test ./agent/ -run 'TestScopeFailClosed|TestCheckScope|TestNetworkBoundaryAccounting|TestRedirectNoDial' -count=1 -v → PASS
go test ./agent/ -run 'TestGuardPanicConvertsPanicToToolError' -count=1  → exit=0; ok
go build ./...                                              → exit=0
git diff --check                                            → exit=0
```

All VIII.4.1 acceptance cases still PASS.

## 3. VIII.4.2 additional suite (all PASS)

```
go test ./agent/ -run 'TestScopeIsolation' -count=1 -v
  TestScopeIsolation_TwoTasks                     PASS
  TestScopeIsolation_ConcurrentOverlap            PASS
  TestScopeIsolation_NoStaleAfterClear            PASS
  TestScopeIsolation_UnseededTaskDenies           PASS (0 dials)
  TestScopeIsolation_CrossTaskDeniedZeroDials     PASS (0 dials)
  TestScopeIsolation_LegacyGlobalDoesNotAuthorizeTasks PASS

go test ./agent/ -run 'TestBashGuard|TestWorkerDefaultTools_ContainsBash|TestDropMCPTools' -count=1
  TestBashGuard_RejectsNetworkCommands            PASS (13 tokens)
  TestBashGuard_AllowsLocalCommands               PASS (10 commands)
  TestBashGuardTool_GuardDecisions                PASS
  TestWorkerDefaultTools_ContainsBash             PASS
  TestDropMCPTools                                PASS

go vet ./agent/   → clean
go build ./...    → exit=0
```

## 4. Control review

| Control | Status | Evidence |
|---|---|---|
| Redirect validation | Active | `agent/httpclient.go` unchanged; `TestRedirectNoDial` PASS |
| Cross-origin credential stripping | Active | unchanged; asserted by redirect tests |
| Dial-level accounting | Active | `withDialCounter` in isolation tests (0 dials on deny) |
| Scope matching for valid scopes | Preserved | VIII.4.1 suite PASS |
| Company fail-closed | Preserved | `TestCheckScope_CompanyFailsClosed` PASS |
| Error categories | Added | sentinels + category tests |
| Provider egress | Unaffected | `agent/provider.go` untouched; norma module not modified |
| Health/auth endpoints | Unaffected | `go test ./server` ok (scoped run); live health 200 on 8799 |
| Detection logic | Untouched | `tools_probe.go`, `tools_http.go`, `tools_diff.go`, `tools_login.go`, `tools_retest.go`, `tools_attackpath.go` unchanged |
| Planner/Worker prompts | Untouched | no prompt/schema edits |
| LLM provider config | Untouched | Phase VIII.3.5 config; no provider changes |
| Existing uncommitted changes | Preserved | `git diff --stat` shows only pre-existing `agent/tools.go` (+16), `agent/worker.go` (+29); backup verified |
| Secrets in source/reports | None | scan for `nvapi-`/`Bearer` → none |

## 5. Functional compatibility

- `SetTaskScope(nil)` semantics: VIII.4.1 changed this to "explicit empty" (ErrScopeEmpty). Worker call sites pass non-empty lists or call `ClearTaskScopeForTask` — production behavior unchanged (verified: `go test ./agent/` green).
- `SetTaskScopeForTask` with `taskID<=0` falls back to the legacy key — no production caller passes <=0.
- New worker behavior: valid scope → per-task authorize; no scope → deny. Workers that previously ran without scope now fail tools that touch the network. This is the intended policy change.

## 6. Strengthened paths (new)

- Cross-task isolation: per-task map + `RunInfo`-keyed resolution.
- Worker-entry enforcement: `defer ClearTaskScopeForTask(taskID)`.
- Containment: Bash guard (heuristic), WebFetch/WebSearch off, MCP dropped.

## 7. Documented limits (unchanged from VIII.4.2 scope)

1. Bash heuristic: `nc` excluded; obfuscated inline network in python/node one-liners; `cmd`/`powershell` inline network tokens.
2. `hostOf` IPv6 parsing (not touched).
3. Process-global legacy store (key "") still exists for non-worker paths.
4. `Worker` concurrent runs: store is now per-task; a single Worker run is still serialized per `execute` (no change).
5. Server-side enrichment (`enrich` package) still performs background DNS/HTTP — out of this phase's mandate, documented in BYPASS_CONTAINMENT.

## 8. Regression verdict (Phase J)

`PASS` — no regressions in valid authorization semantics; the only functional change is the intended fail-closed policy + containment.

## 9. Post-phase hash continuity

`backups\phase8_4_2\phase-change-hashes.txt`:
- PRE (pre-edit, captured at phase start): scopecheck.go `FA8B8601…`, httpclient.go `FB3BA477…`, worker.go `F59F2BAD…` (match VIII.4.1 post-hashes; backup copies verified).
- NEW files: NO-BACKUP-NEW-FILE (created this phase).
- POST hashes appended by Phase H (runtime validation).
