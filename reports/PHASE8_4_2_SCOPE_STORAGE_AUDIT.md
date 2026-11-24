# PHASE8_4_2 — SCOPE STORAGE AUDIT (Phase B)

## 1. Storage implementation (as found)

`agent/scopecheck.go` (pre-VIII.4.2):

```go
var (
	scopeMu      sync.RWMutex
	scopeEntries []db.TaskScope
	scopeSeeded  bool
)
```

- One process-global store guarded by `sync.RWMutex`.
- `SetTaskScope` / `ClearTaskScope` mutate it; `scopeState()` reads (RLock).

## 2. Answers to the ten audit questions

| # | Question | Answer (with evidence) |
|---|---|---|
| 1 | Where is scope stored? | Process-global vars `scopeEntries` + `scopeSeeded` in `agent/scopecheck.go:60-64` (pre-VIII.4.2) |
| 2 | Is storage process-global? | YES — no task ID in the store; keyed by nothing |
| 3 | Is it protected by a mutex? | YES — `sync.RWMutex scopeMu` |
| 4 | Is scope keyed by task ID? | NO (pre-fix). After VIII.4.2: `map[string]taskScopeState` keyed by `scopeKeyForTask(taskID)` |
| 5 | Do multiple workers share scope? | YES — process-wide: every Worker run reads the same store |
| 6 | Can concurrent tasks overwrite each other? | YES — `SetTaskScope(scopes)` in `worker.go:375` is unconditional; last writer wins. Overlap is possible (workers run with 3 parallel slots per `workers=3`). |
| 7 | Can stale scope survive task completion? | YES — nothing deleted the entry; `ClearTaskScope` only cleared the single global slot when a worker without scope ran (and other workers could then clobber it). `Worker.execute`'s defer was absent pre-fix. |
| 8 | Do cancellation/error paths clean up? | Not present pre-fix (no defer; no error path handling). |
| 9 | Can a Worker inherit another task's scope? | YES — a task without scope gets `ClearTaskScope()`, but an interleaved task's scope remains in the store; the next request from the no-scope worker would see it. |
| 10 | Do task retries reuse/reconstruct auth? | Retries re-enter `execute`; `SetTaskScope(scopes)` is called again if scope exists (idempotent); otherwise `ClearTaskScope()`. Not isolated per-task pre-fix. |

## 3. Execution path trace (actual, pre-fix)

```
POST /api/tasks → server.go:createTask → goals.go:launchTask → engine.Run
  → server/task_llm.go:agentsForTask → agent.NewWorker(workerRuntime,...)
  → Worker.execute (agent/worker.go:361)
      ├─ as.ListTaskScopeWithSources(taskID)
      ├─ SetTaskScope(scopes)            (worker.go:375)  ← process-global write
      └─ (no cleanup on exit)
        ├─ [any tool] → doHTTP(ctx, url) → scopeGuard(rawURL) → CheckScope
        │                  (no task identity — any worker's scope matches)
        └─ [redirect] → CheckRedirect → scopeGuard(req.URL.String())
```

## 4. Mutex alone ≠ isolation

`scopeMu` prevents data races; it does not prevent logical leakage: while Worker A (task 100, scope 127.0.0.1:3004) and Worker B (task 200, scope 10.00.0.0/8) overlap, B's `doHTTP` to `127.0.0.1:3004` sees A's scope and is allowed, or B's scope is visible to A.

## 5. VIII.4.2 design applied

- Store: `map[string]taskScopeState` keyed by task registry id (string); `""` reserved for legacy/non-task callers.
- `Worker.execute` uses `SetTaskScopeForTask(taskID, ...)` / `ClearTaskScopeForTask(taskID)` + `defer ClearTaskScopeForTask(taskID)`.
- `scopeGuard(ctx, ...)` resolves `scopeKeyFromContext(ctx)` — the ctx Worker passes to tools carries `RunInfo{TaskID}` (worker.go:406 → capture.go:37,80 → norma `Session.Prompt(ctx, ...)`).
- `CheckScope(rawURL)` remains as the legacy global-key view; `CheckScopeForKey(key, rawURL)` is the task-scoped check.
- No cross-task reads: `doHTTP` with a task key reads only `scopes[key]`.

## 6. Verification

- `agent/scope_task_isolation_test.go`: two-task allow/deny, concurrent overlap (start barrier), stale-after-clear, cross-task zero-dials, unseeded denial, legacy-global not visible to tasks.
- Verified: `go test ./agent/ -run TestScopeIsolation -count=1 -v` — all PASS (see Phase I / Test Results).

## 7. Impact on existing behavior

- Valid scope semantics preserved (root_domain/subdomain/ip/cidr matching unchanged; company remains non-authorizing).
- VIII.4.1 guarantees preserved (fail-closed matrix, zero-dial proofs, redirect revalidation, credential stripping, dial accounting) — re-run in VIII.4.2 (see `PHASE8_4_2_REGRESSION_REVIEW.md`).
- `SetTaskScope(nil)` semantics changed to "explicit empty" (VIII.4.1); worker call sites always pass a non-empty list or call Clear — no production impact observed.

## 8. Remaining contamination (pre-fix, historical)

Documented in Phase C: the process-global store made stale scope observable across tasks when workers run concurrently (`workers=3` default). VIII.4.2 removes the read path; full per-worker isolation under concurrency is proven by `TestScopeIsolation_ConcurrentOverlap` (both workers start on a barrier and each verifies the other's destination is denied).
