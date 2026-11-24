# PHASE8_4_2 — WORKER ENFORCEMENT (Phase D)

## 1. Enforcement boundary (actual)

Worker entry (`agent/worker.go:361` `Worker.execute`) enforces authorization before any tool runs:

```go
// VIII.4.2 — per-task storage ...
if as != nil {
	if scopes, err := as.ListTaskScopeWithSources(taskID); err == nil && len(scopes) > 0 {
		SetTaskScopeForTask(taskID, scopes)
	} else {
		ClearTaskScopeForTask(taskID)   // missing scope → Deny
	}
} else {
	ClearTaskScopeForTask(taskID)
}
defer ClearTaskScopeForTask(taskID)
```

So at the Worker boundary:
1. Missing scope → `ClearTaskScopeForTask` → any subsequent `doHTTP` returns `ErrScopeMissing`.
2. Empty scope (`ListTaskScopeWithSources` returns empty) → `ClearTaskScopeForTask`.
3. Invalid scope (malformed DB rows) → `SetTaskScopeForTask` → `CheckScopeForKey` returns `ErrScopeMalformed` on first request (fail-closed).
4. Valid scope → stored under the task key; only that task's ctx key can read it.
5. Cancellation/error: `defer ClearTaskScopeForTask(taskID)` runs on exit.

## 2. Tool execution path (verified)

Every target-assessment tool in the worker calls `doHTTP(ctx, ...)` with the run ctx:

- `agent/tools_http.go:112,219` (HTTPRequest, Session.auth_check)
- `agent/tools_probe.go:181,303,494` (oracles + Crawl)
- `agent/tools_diff.go:88,260` (IdorDiff, Fuzz)
- `agent/tools_login.go:103` (Login)
- `agent/tools_attackpath.go` (AttackPath)

`doHTTP` → `scopeGuard(ctx, rawURL)` → `CheckScopeForKey(scopeKeyFromContext(ctx), rawURL)` before `client.Do`. The redirect `CheckRedirect` revalidates every hop via the same guard (VIII.4.1).

## 3. Required behaviours vs. runtime evidence

| Requirement | Evidence |
|---|---|
| Task without valid scope cannot enter target-assessment execution | `TestScopeIsolation_UnseededTaskDenies` (unit, 0 dials); runtime: Case A of worker runtime (see `PHASE8_4_2_WORKER_RUNTIME.md`) — no bench hits. |
| Worker cannot reconstruct unrestricted scope | Code: `ClearTaskScopeForTask` on missing; `dropMCPTools`; guard on Bash. No reconstruction paths. |
| Worker cannot inherit another task's scope | `TestScopeIsolation_TwoTasks`, `TestScopeIsolation_LegacyGlobalDoesNotAuthorizeTasks`. |
| Rejected task is not a successful result | `doHTTP` returns error; tools propagate (`actool.Errorf`); no bypass. |
| Authorization errors distinguishable | `ErrScopeMissing` vs `ErrScopeDenied` vs `ErrScopeEmpty` vs `ErrScopeMalformed`. |
| Valid authorized tasks preserve behavior | `TestScopeIsolation_TwoTasks` (200 + body), `TestScopeFailClosed_ValidScopes`, plus VIII.4.1 compatibility suite. |

## 4. Not modified (as required)

- LLM prompts (planner/worker system prompts), tool schemas, provider selection, detection logic: untouched by this phase.

## 5. Gaps

- Worker-level live denial is `PARTIAL` — proven by unit tests (real path) and will be strengthened by runtime validation in Phase I (`PHASE8_4_2_WORKER_RUNTIME.md`). The new binary is being validated end-to-end on port 8799.
