# PHASE8_4_2 — CONCURRENCY TESTS (Phase E)

Deterministic local regression tests for cross-task isolation. All run through the real path: `doHTTP` → `scopeGuard` → `CheckScopeForKey`; the dial layer is the boundary oracle (`withDialCounter`, `agent/http_connection_counter_test.go:20`).

## 1. Test scenarios

Two independent loopback hosts are used so host-based scopes can distinguish them:

```go
func newLoopbackServerOn(t, addr, body)  // agent/scope_task_isolation_test.go
```

### TestScopeIsolation_TwoTasks

Two scopes: Task 101 → `ip` `Net: 127.0.0.2/32` (server A, body "A"); Task 202 → `ip` `Net: 127.0.0.3/32` (server B, body "B").

Checks (PASS):
1. Task A (`WithRunInfo(ctx, RunInfo{TaskID: 101})`) → `doHTTP(ctxA, srvA.URL)` → 200, body "A".
2. Task A → `doHTTP(ctxA, srvB.URL)` → `errors.Is(err, ErrScopeDenied)`.
3. Task B → `doHTTP(ctxB, srvB.URL)` → 200, body "B".
4. Task B → `doHTTP(ctxB, srvA.URL)` → `ErrScopeDenied`.

Proves: each Worker retains its own authorization; the other task's destination is denied.

### TestScopeIsolation_ConcurrentOverlap (the barrier test)

Two goroutines, both parked on a `start` channel closed after both have registered:

```
close(start) → both workers run simultaneously
```

Each worker:
- asserts its **own** destination is reachable (`doHTTP` → 200 + expected body),
- asserts the **other** task's destination is denied (`ErrScopeDenied`).

Failures are collected on a channel. Proves deterministic overlap + no cross-talk (the barrier removes timing luck).

### TestScopeIsolation_NoStaleAfterClear

- Seed Task 401 → request allowed.
- `ClearTaskScopeForTask(401)` → `doHTTP` → `ErrScopeMissing`.

Proves: completion of one task's work does not leave usable authorization behind.

### TestScopeIsolation_UnseededTaskDenies

- Task 501 with **no** store entry → `doHTTP` → `ErrScopeMissing`; dial counter == 0.

### TestScopeIsolation_CrossTaskDeniedZeroDials

- Task 601 seeded (A). Task 602 has **no** store entry.
- `doHTTP(ctxB602, srvB)` → `ErrScopeMissing`; dial counter delta == 0.

Proves: an unscoped task cannot dial anything.

### TestScopeIsolation_LegacyGlobalDoesNotAuthorizeTasks

- Legacy global scope seeded via `SetTaskScope` (key "").
- `doHTTP` with `RunInfo{TaskID: 701}` → `ErrScopeMissing` (legacy store must not authorize tasks).

## 2. Negative cases (required by Phase E)

| Negative case | Representation | Result |
|---|---|---|
| Task A has no scope | no `SetTaskScopeForTask(A, ...)` call | A denied (`ErrScopeMissing`) |
| Task B has empty scope | `SetTaskScopeForTask(B, nil)` | B denied (`ErrScopeEmpty`) |
| Task A malformed scope | `{Kind:"ip", Value:"garbage"}` | A denied (`ErrScopeMalformed`) |
| Task B valid + overlap | `TestScopeIsolation_ConcurrentOverlap` | cross denied |
| One task cancelled while other continues | (unit) — `ClearTaskScopeForTask` on exit; `defer` in `Worker.execute` | isolation verified by `NoStaleAfterClear`/`CrossTaskDeniedZeroDials` |
| One task completes while other active | (unit) — store keyed by task; no shared slot | verified by TwoTasks/ConcurrentOverlap |

## 3. Synchronization (non-timing-based)

- Deterministic overlap via a start barrier (`start := make(chan struct{})`, `close(start)` after both goroutines are launched).
- No sleeps; dial counts asserted exactly (`atomic.LoadInt32`).

## 4. Where the behavior lives

- Store: `agent/scopecheck.go` — `map[string]taskScopeState`, `scopeKeyForTask`, `SetTaskScopeForTask`, `ClearTaskScopeForTask`, `CheckScopeForKey`.
- Worker wiring: `agent/worker.go:373-382` + `defer ClearTaskScopeForTask(taskID)`.
- Resolution from ctx: `scopeKeyFromContext` via `RunInfoFrom(ctx).TaskID` (set at `agent/worker.go:406`).

## 5. Executed commands (evidence)

```
go test ./agent/ -run 'TestScopeIsolation' -count=1 -v
  → PASS (6/6): TwoTasks, ConcurrentOverlap, NoStaleAfterClear, UnseededTaskDenies, CrossTaskDeniedZeroDials, LegacyGlobalDoesNotAuthorizeTasks
```

## 6. Verdict for this report

**PASS** — the deterministic concurrency/isolation suite is green; live two-worker overlap is `NOT LIVE-VERIFIED` (see `PHASE8_4_2_FINAL.md`).
