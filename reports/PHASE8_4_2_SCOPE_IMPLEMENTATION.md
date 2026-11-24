# PHASE8_4_2 — SCOPE IMPLEMENTATION (Phase C)

## 1. Design decision

The VIII.4.1 design ("process-global scope store") was replaced with **task-scoped authorization**:

- A `map[string]taskScopeState` in `agent/scopecheck.go`, keyed by a scope key derived from the task registry id.
- The scope key is resolved from the `RunInfo` context value that the Worker already attaches (`agent/worker.go:406`, `agent/runinfo.go:37-46`), so **no execution-path parameter threading** was required through the external norma `agentcore`.

### Why this is bounded

- `Worker.execute` is the single place where workers are started; it already has the task ID and the ctx. All `doHTTP` calls in the worker path already pass ctx (`agent/tools_http.go:112,219`, `agent/tools_probe.go:181,303,494`, `agent/tools_diff.go:88,260`, `agent/tools_login.go:103`), so resolving the task key from ctx reaches every target-assessment path.
- `agentcore` (norma), `tool`, and `permission` are external modules; no provider or infrastructure dependencies were modified.

## 2. New API (agent package)

```go
// VIII.4.2 — per-task scope store
func SetTaskScopeForTask(taskID int64, entries []db.TaskScope)
func ClearTaskScopeForTask(taskID int64)
func CheckScopeForKey(key string, rawURL string) error   // internal: scopecheck.go
func scopeKeyForTask(taskID int64) string
func scopeKeyFromContext(ctx context.Context) string
```

Legacy compatibility (kept for tests and non-worker paths):

```go
func SetTaskScope(entries []db.TaskScope)  // key ""
func ClearTaskScope()                      // key ""
func CheckScope(rawURL string) error      // key ""
```

`scopeGuard(ctx, rawURL)` is the single chokepoint used by `doHTTP` (initial) and the redirect revalidation, both in `agent/httpclient.go`.

## 3. Sentinel errors (VIII.4.1, unchanged, now used per-task)

`ErrScopeMissing`, `ErrScopeEmpty`, `ErrScopeMalformed`, `ErrScopeDenied` — each task-scoped check returns one of these; `errors.Is` distinguishes them.

## 4. Worker wiring (agent/worker.go)

```go
// VIII.4.2 — per-task storage: the scope is keyed by this task's id, is
// dropped when the run ends, and cannot be inherited by another task.
if as != nil {
	if scopes, err := as.ListTaskScopeWithSources(taskID); err == nil && len(scopes) > 0 {
		SetTaskScopeForTask(taskID, scopes)
	} else {
		ClearTaskScopeForTask(taskID)
	}
} else {
	ClearTaskScopeForTask(taskID)
}
defer ClearTaskScopeForTask(taskID)
```

`ListTaskScopeWithSources` (pre-existing) returns the task's scope rows; empty → clear → **deny**.

## 5. Containment changes

See `reports/PHASE8_4_2_BYPASS_CONTAINMENT.md`: Bash guard, WebFetch/WebSearch disabled, MCP tools dropped from the worker surface.

## 6. Validation strategy

`agent/scope_task_isolation_test.go` + `agent/bash_guard_test.go` + Phase H/I runtime validation (new binary on 127.0.0.1:8799).

## 7. Changed files

```
agent/scopecheck.go          CHANGED  (task-scoped store, VIII.4.2 API, CheckScopeForKey)
agent/httpclient.go          CHANGED  (scopeGuard(ctx, url) ×2)
agent/worker.go              CHANGED  (Set/ClearTaskScopeForTask + defer clear + Bash guard + WebFetch/WebSearch off + MCP drop)
agent/bash_guard.go          NEW
agent/bash_guard_test.go     NEW
agent/scope_task_isolation_test.go  NEW
```

Pre/post hashes: `backups\phase8_4_2\phase-change-hashes.txt`.
