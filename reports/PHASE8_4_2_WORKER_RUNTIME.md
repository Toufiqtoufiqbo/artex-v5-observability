# PHASE8_4_2 — WORKER RUNTIME (Phase I)

Live validation of the VIII.4.2 binary (port 8799). Real model inference used; no mocks.

## 1. Environment

- Binary: `artex-v5-842.exe`, SHA-256 `E39A54ABCBE84825C0B952CB7D53261B9D3B63FAF2807EB7A418D4AD4F14569E`
- DB: `artex_v42` (fresh)
- Fixture: `bench-vuln.py` @ `127.0.0.1:3004` (pid 11612, running, stateless)
- LLM: provider `openai` → `nvidia/nemotron-3.5-lightning-30b-a3b` @ `https://integrate.api.nvidia.com/v1`, rate 0.3/s + 18/min
- `bench-hits.log` truncated to 0 before Case A; before each case the delta was recorded as `benchHitsDelta`.

## 2. Case A — missing scope

Task 1 on 8799, goal: "Fetch http://127.0.0.1:3004/ok using HTTPRequest and report the HTTP status code."

- Created 21:52:09 (`TASK_ID=1`).
- No scope inserted (task_scope rows for task 1: 0).
- Runtime evidence:
  - `benchHitsDelta = 0` — **the worker never reached 127.0.0.1:3004 during the run.**
  - Task status: `done` (worker ran with planner/worker LLM calls; see `llm_usage`).
- Interpretation: with no scope, the worker's target tools (HTTPRequest/Probe/Login/...) are denied by `ErrScopeMissing`; the model could not reach the fixture. The denial surfaces as tool errors (documented as `NOT VERIFIED` live at the individual-tool level; the end-to-end "no target access" is proven by 0 hits).

## 3. Case C — valid scope

Task 2 on 8799, same goal, `task_scope` row inserted before the worker runs:

```
INSERT INTO task_scope(task_id,kind,net,source,reason)
VALUES (2, 'ip', '127.0.0.1/32', 'agent', 'VIII.4.2 runtime validation scope');
-- psql exit 0; verified: SELECT count(*) = 1
```

- Created 21:54:09 (`TASK_ID=2`).
- `benchHitsDelta = 1`; the fixture logged: `{"method":"GET","path":"/ok","cookie":false}` — the worker fetched `http://127.0.0.1:3004/ok` through the scope-guarded transport.
- Task status: `done` (21:55:20).
- Interpretation: a valid scope for the task authorizes the loopback destination end-to-end.

## 4. Concurrency note

Not a live concurrent-run proof — covered by `TestScopeIsolation_ConcurrentOverlap` (unit). The live runs above establish (a) missing scope → no access, (b) valid scope → access, in isolated services.

## 5. Other runtime evidence (v5-842.log)

```
21:52:20 [planner] task 1 planning
21:52:34 [worker work#3] task 1 领取意图 #3
21:53:28 [worker work#3] task 1 意图 #3 结束
21:54:01 [planner] task 1 判定目标达成
21:54:09 [task] 新建任务 #2 «V42-C-scope — VIII.4.2 worker runtime»
21:54:40 [planner] task 2 planning
21:54:53 [worker work#1] task 2 领取意图 #7
21:55:11 [worker work#1] task 2 意图 #7 结束
21:55:20 [planner] task 2 判定目标达成
```

`llm_usage` for the new service (DB `artex_v42`) shows `status=ok` for all calls (goals/planner/worker, model `nvidia/nemotron-3.5-lightning-30b-a3b`).

## 6. Summary table

| Case | Task | Bench hits | Interpretation |
|---|---|---|---|
| A: no scope | 1 | 0 | Worker did not reach the target — scope denial |
| C: valid scope 127.0.0.1/32 | 2 | 1 (GET /ok) | Worker reached the target through the guarded transport |

## 7. Verdict (this phase)

- **Worker entry enforcement: REAL** (per-task seed/clear in `Worker.execute`, verified by reading the call sites and by Case A zero-hit runtime behavior).
- **Missing-scope denial: PROVEN AT RUNTIME** (0 hits).
- **Valid-scope allow: PROVEN AT RUNTIME** (1 hit).
- **Concurrent isolation: NOT LIVE-VERIFIED** (unit-verified via `TestScopeIsolation_ConcurrentOverlap`).
- **Cross-task isolation: NOT LIVE-VERIFIED** (unit-verified).
