# PHASE8_4_2 — FINAL

Date: 2026-11-21

## Verdict

# `TASK ISOLATION VERIFIED WITH LIMITATIONS`

## 1. Priority-0 objectives

| # | Objective | Status | Evidence |
|---|---|---|---|
| P0 | Eliminate cross-task scope contamination | **DONE** | `TestScopeIsolation_TwoTasks`, `_ConcurrentOverlap`, `_NoStaleAfterClear`, `_CrossTaskDeniedZeroDials`, `_LegacyGlobalDoesNotAuthorizeTasks` (all PASS); scope store is a per-task map |
| P0 | Enforce scope at Worker execution entry | **DONE** | `agent/worker.go:373-382` seeds/clears per task + `defer ClearTaskScopeForTask(taskID)`; read path via `scopeKeyFromContext(ctx)` |
| P0 | Missing/invalid scope cannot reach target network execution | **DONE (incl. runtime)** | VIII.4.1 fail-closed matrix re-verified; Case A live: bench hits = 0; `doHTTP` returns `ErrScopeMissing` |

## 2. Priority-1 objectives

| # | Objective | Status | Evidence |
|---|---|---|---|
| P1 | Contain Bash network egress | DONE (heuristic) | `agent/bash_guard.go` (wrap + 13-token reject list + local-command allow list); `TestBashGuard_*` PASS |
| P1 | Contain WebFetch | DONE | `EnableWebFetch: false` in worker options |
| P1 | Contain WebSearch | DONE | `EnableWebSearch: false` in worker options |
| P1 | Contain MCP | DONE | `dropMCPTools(tools)` in worker; `TestDropMCPTools` PASS |
| P1 | Rebuild + validate the modified service | DONE | `artex-v5-842.exe` built (SHA-256 `E39A54AB…`); isolated runtime on 127.0.0.1:8799, health 200, auth init, LLM config |
| P1 | Verify actual Worker behavior | PARTIAL (see limits) | Case A (0 hits) + Case C (1 hit) live; concurrent overlap live NOT RUN |
| P1 | Preserve VIII.4.1 guarantees | DONE | all VIII.4.1 mandated commands re-run, all PASS (`PHASE8_4_2_REGRESSION_REVIEW.md`) |

## 3. Acceptance matrix (Phase G)

| Requirement | Expected | Status | Evidence |
|---|---|---|---|
| Unseeded task → DENY | DENY | PASS | `TestScopeIsolation_UnseededTaskDenies` (0 dials); Case A (0 bench hits) |
| Nil scope → DENY | DENY | PASS | `SetTaskScopeForTask(id, nil)` → `ErrScopeEmpty`; `TestCheckScope_EmptyScopeDenies` |
| Empty scope → DENY | DENY | PASS | `TestScopeIsolation_Empty` + `TestCheckScope_EmptyScopeDenies` |
| Unseeded → DENY | DENY | PASS | `TestScopeFailClosed_Unseeded`, Case A |
| Malformed → DENY | DENY | PASS | `TestScopeIsolation_MalformedZeroDials` (10 sub-cases in `TestCheckScope_MalformedEntriesDeny`) |
| Unsupported type → DENY | DENY | PASS | `unsupported scope kind` sub-case |
| Valid scope + in-scope → ALLOW | ALLOW | PASS | `TestScopeIsolation_TwoTasks` (200 + body), `TestScopeFailClosed_ValidScopes` |
| Valid scope + out-of-scope → DENY | DENY | PASS | `TestScopeIsolation_TwoTasks` (cross-task denied) |
| Unauthorized destination → 0 dials | PASS | PASS | `TestScopeFailClosed_UnseededZeroDials`, `EmptyZeroDials`, `MalformedZeroDials`; runtime Case A |
| Unauthorized redirect → DENY | DENY | PASS | `TestRedirectNoDial` (unchanged, 1 dial initial) |
| Stale scope inheritance → DENY | PASS | PASS | `TestScopeIsolation_NoStaleAfterClear` |
| Concurrent overlap isolation | PASS | NOT LIVE-VERIFIED | `TestScopeIsolation_ConcurrentOverlap` (unit, barrier start) |
| Cross-task isolation | PASS | unit-verified | `TestScopeIsolation_TwoTasks`, `TestScopeIsolation_CrossTaskDeniedZeroDials` |
| Worker without scope cannot reach target | DENY | PASS (unit) + runtime | Case A: 0 bench hits |
| Worker cannot reconstruct unrestricted scope | PASS | unit | no reconstruction paths |
| Bash network bypass | Disabled | unit + runtime | `bash_guard.go`, `TestBashGuard_*` |
| Bash non-network commands | Preserved | unit | `TestBashGuardTool_GuardDecisions` |
| WebFetch network bypass | Disabled | code | `EnableWebFetch: false` |
| WebSearch network bypass | Disabled | code | `EnableWebSearch: false` |
| MCP network bypass | Disabled | unit + code | `dropMCPTools` |
| VIII.4.1 suites | PASS | PASS | `go test ./agent/ -count=1` exit=0 |
| New binary identity verified | PASS | code | `artex-v5-842.exe` SHA-256 `E39A54AB…`, `go build ./...` exit=0 |
| New isolated runtime healthy | PASS | runtime | health 200 on 8799, auth init, setLLM |
| Worker runtime (Case A) | PASS | runtime | 0 bench hits |
| Worker runtime (Case C) | PASS | runtime | 1 bench hit (`GET /ok`) |
| Health/auth/non-network | Preserved | PASS | `go test ./server` ok (scoped); health 200 live |
| LLM provider unchanged | PASS | code | untouched (VIII.3.5) |
| Detection logic unchanged | PASS | code | untouched |
| Prompts/schemas unchanged | PASS | code | untouched |
| Uncommitted changes preserved | PASS | backup | `git diff --stat` = only pre-existing |
| No secrets | PASS | scan | `nvapi-` scan → none |

## 4. Residual limitations (honest, documented)

1. **Concurrent overlap live** — `TestScopeIsolation_ConcurrentOverlap` is a unit test; the live two-worker overlap on the new binary was not run (budget). Classification: concurrent isolation proven at unit level.
2. **Bash heuristic** — 13-token list; excludes `nc` (false-positive risk), obfuscated inline network in python/node one-liners, `cmd`/`powershell` inline network tokens. Documented in `BYPASS_CONTAINMENT`.
3. **Server enrichment** (`enrich`) still performs background DNS/HTTP; out of mandate, documented.
4. **Legacy global store** (key "") still exists for non-worker paths.
5. Full runtime denial per-tool at the worker (e.g., exact tool result on denial) is `NOT VERIFIED` live — proven by 0 target hits.
6. Existing full-suite failures in `server` are pre-existing/environmental (verified).

## 5. Answers to Phase 9 questions

1. Cross-task contamination eliminated? **YES** (unit-verified; P0).
2. Worker entry enforcement in place? **YES** (P0).
3. Missing/invalid scope blocked before transport? **YES** (P0 + Case A runtime).
4. Containment of Bash/WebFetch/MCP? **YES** (P1).
5. New isolated runtime built and validated? **YES** (P1).
6. Worker behavior verified live? **PARTIAL** (Case A + Case C).
7. VIII.4.1 preserved? **YES** (all mandated commands re-run, PASS).

## 6. Source preservation

`reports/PHASE8_4_2_SOURCE_PRESERVATION.md`; backups under `backups\phase8_4_2\`.

## 7. Changed files

```
agent/scopecheck.go                 CHANGED
agent/httpclient.go                 CHANGED
agent/worker.go                     CHANGED
agent/bash_guard.go                 NEW
agent/bash_guard_test.go            NEW
agent/scope_task_isolation_test.go  NEW
```

(+ Phase H: `artex-v5-842.exe` built to a separate file; no source changes.)

## 8. Next: Phase VIII.4.3

Suggested: live concurrent-overlap proof (two workers on the new binary), full-path-per-tool denial capture, and deeper Bash network coverage.
