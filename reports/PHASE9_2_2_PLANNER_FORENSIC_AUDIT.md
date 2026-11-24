# PHASE IX.2.2 — GATE A
## PLANNER FORENSIC AUDIT (READ-ONLY)

**Generated:** 2026-11-24
**HEAD:** `e6ec56999175509551a1e43a3ee3b053d0941bd9` (detached)
**Working tree before this gate:** 95 entries — 30 modified, 65 untracked (IX.2.1 state preserved).
**Scope of this document:** observation only. No application source was modified to produce it.

Evidence labels used: `SOURCE VERIFIED`, `CALL-PATH VERIFIED`, `TEST VERIFIED`, `INTEGRATION VERIFIED`, `RUNTIME VERIFIED`, `PARTIALLY VERIFIED`, `UNKNOWN`, `NOT INVESTIGATED`.

---

## A1. Planner implementation (source verified)

### A1.1 Type and ownership

`agent/planner.go:23` — `type Planner struct` holds provider, model, transcript store, compaction window, max turns, kill/steer callbacks, proxy, web-search options, constraint-injection resolver, compactor, and a per-task `TodoStore` map (`planner.go:47-48`).

It has **no Guard field** and **no plan/intent type of its own**. It is an LLM agent wrapper, not a plan compiler.

`agent/planner.go:355` — `func (p *Planner) Plan(ctx, taskID, as, ts, goal, triggers, emit) (met bool, reason string, err error)`.

`SOURCE VERIFIED`.

### A1.2 What `Plan` actually does

1. `p.compactor.OnPlannerRound` (`planner.go:360`) — bookkeeping only.
2. Builds a `ToolSet` bound to the exploration (`planner.go:361-372`), including `SetFindingRecorder`, asset store, task id, coverage toggle, kill/steer callbacks, owner node.
3. Assembles tools: `PlannerTools()` + `actool.DefaultTools()`, then `AugmentTools`, then `ContainLocalShell` and `ContainFileTools` (`planner.go:375-381`).
4. Builds the situational block from trigger events + a pre-fetched `graph_overview` (`planner.go:385`).
5. Builds the system prompt from the editable template `plannerDefaultTmpl` (`planner.go:298-340`) plus `constraintBlock` when enabled (`planner.go:397-399`).
6. Runs one `agentcore` session via `captureRun` (`planner.go:468-474`).
7. Returns `tsx.GoalMet`, `tsx.Reason` — which are set by the `prove_goal` / `goal_met` tools, not by `Plan` itself (`agent/tools.go:1157-1218`).

**There is no plan object, no plan validation step, no intent list returned to the caller.** The only outputs are side effects of tool calls (`add_intent`, `prove_goal`, `steer_work`, `kill_work`, `TodoWrite`).

`SOURCE VERIFIED`.

### A1.3 Prompt construction and parsing

- The planner prompt is a single editable body rendered through `renderSystem("planner", plannerDefaultTmpl, PlannerVars{...})` (`agent/planner.go:342-345`).
- Deduplication is **prompt text only**:
  - `planner.go:304` — "换措辞重复生成已存在的意图是严重错误"
  - `planner.go:323-329` — "已有 open/running 覆盖 → 不再生成"; recent_done handling.
  - `planner.go:336` — "去重主要靠它与已有意图比对"
- There is no structured output parser: the model calls tools; the tool handlers validate.

`SOURCE VERIFIED`.

### A1.4 Goal decomposition

- `agent/goals.go:110` — `DecomposeGoalsWithProvider` runs a separate one-shot agent with `set_goals` and `set_constraints` tools (`goals.go:133-140`), `MaxTurns: 8`.
- Called from `server/goals.go:387` (`createGoals`) during task creation.
- Goals are persisted by the `set_goals` tool; the caller reads them back (`goals.go:166-181`).
- If the LLM yields nothing, `createGoals` falls back to the raw goal as one goal node (`server/goals.go:394-407`).
- This is **one-shot at task creation**, not a continuous decomposition loop.

`CALL-PATH VERIFIED`.

### A1.5 Hooks, guard and containment

| Surface | Guard hooks | Shell containment | FS containment | Evidence |
|---|---|---|---|---|
| Worker | **yes** — `steerHooks{inner: t.Guard.Hooks()}` | via tool assembly | via tool assembly | `server/engine.go:1009-1015` |
| Chat / main agent | **yes** — `opts.Hooks = c.guard.Hooks()` | yes | yes | `agent/chat.go:167-169` |
| Planner | **no** — `agentcore.Options` in `Plan` sets no `Hooks` | **yes** — `ContainLocalShell` | **yes** — `ContainFileTools` | `agent/planner.go:408-443`, `planner.go:381` |

This is a real asymmetry: user-configured intercept rules (`guard.applyIntercept`) are evaluated for worker and chat tool calls, but **not for the planner's own tool calls** (its optional Bash probes and WebFetch). The planner's shell is still constrained by `agent/bash_guard.go:53` (`ContainLocalShell`) and its file tools by `agent/fs_containment.go:128`, so this is not an unrestricted-shell gap — it is a **policy-layer gap**.

`SOURCE VERIFIED`.

### A1.6 Retries, cancellation, execution feedback

- Engine owns worker retries: `modelErrorRetryPolicy` (`server/llmretry.go:67`), loop at `server/engine.go:1030-1046`, bounded by config; backoff via `sleepCtx`.
- Engine owns intent settlement: `server/engine.go:1135-1156` (`done` / `blocked` / `exhausted` / `stopped`).
- Planner cancellation: task pause/delete cancels `ectx`; `Plan` has no retry loop of its own.
- Feedback into planning: `TriggerEvent` (`agent/planner.go:158-167`) rendered by `renderTriggers` (`planner.go:173-215`), including worker output and yielded fact ids.
- Planner wake-up is debounced and heartbeat-driven (`server/engine.go:800-880`, `plannerHeartbeatInterval` at `781-787`).

`CALL-PATH VERIFIED`.

---

## A2. Real call graph

```
User objective
  └─ server.Manager.CreateTaskWithOptions
       └─ server.createGoals            server/goals.go:371
            └─ agent.DecomposeGoalsWithProvider   agent/goals.go:110
                 └─ [LLM] set_goals / set_constraints tools
                      └─ ExplorationStore.AddNode(kind='goal')   db/exploration.go:197

Engine.Run                          server/engine.go:748
  ├─ plannerLoop                     server/engine.go:800
  │    └─ runRound (debounce + guards)
  │         └─ Planner.Plan          agent/planner.go:355
  │              └─ [LLM] add_intent  agent/tools.go:1091
  │                   └─ addOneIntent agent/tools.go:1032
  │                        ├─ validate summary / parent kind
  │                        ├─ CheckAssetsIntercept  agent/tools.go:1054
  │                        └─ ExplorationStore.AddIntent  db/exploration.go:220
  │                             └─ AddNode(kind='intent', state='open')  db/exploration.go:197
  │                                  └─ INSERT exploration_nodes + exploration_anchors
  └─ workerLoop → claimNext          server/engine.go:1237
       └─ Frontier                    db/exploration.go:1136   (ORDER BY priority DESC, id ASC)
            └─ ClaimIntent            db/exploration.go:1173   (open → running)
                 └─ runIntent         server/engine.go:982
                      └─ Worker.Execute / ExecuteWithMessage  agent/worker.go
```

**Single intent-creation point:** `ExplorationStore.AddIntent` (called by `addOneIntent`; tests and the benchmark seed intents through the same method). `addOneIntent` is the only production caller that validates before writing.

`CALL-PATH VERIFIED`.

---

## A3. Existing functionality inventory

| Capability | Status | Evidence |
|---|---|---|
| Goal decomposition | `EXISTS AND INTEGRATED` | `agent/goals.go:110`, `server/goals.go:387` |
| Subtask / intent generation | `EXISTS AND INTEGRATED` | `agent/tools.go:1091`, `db/exploration.go:220` |
| Task prioritization | `EXISTS BUT PARTIALLY INTEGRATED` | `priority` column + frontier ordering (`db/exploration.go:1140-1142`); the planner prompt never explains priority and the default is 5 (`agent/tools.go:1047-1050`) |
| Structured intent recognition | `NOT IMPLEMENTED` | intent payload is `{summary, asset_ids}` only (`agent/tools.go:1067-1070`) |
| Dependency detection / enforcement | `NOT IMPLEMENTED` | no dependency edge; `ck_edge_rel` allows only `spawns, derived_from, yields, proves, covers` (`db/schema.sql`) |
| Duplicate intent detection | `NOT IMPLEMENTED` | prompt-only (`agent/planner.go:304,323-336`); no code check |
| Plan validation | `EXISTS BUT PARTIALLY INTEGRATED` | summary non-empty, parent kind, asset intercept (`agent/tools.go:1033-1066`); no schema/dependency/budget validation |
| Plan revision | `EXISTS BUT PARTIALLY INTEGRATED` | re-planning on triggers; no explicit revision record or determinism |
| Historical context reuse | `EXISTS AND INTEGRATED` | `graph_overview`, `recent_done`, persistent `TodoStore` (`agent/planner.go:436,460-465`) |
| Task completion tracking | `EXISTS AND INTEGRATED` | intent states + goal states (`db/schema.sql` `ck_node_state`) |
| Execution feedback | `EXISTS AND INTEGRATED` | `TriggerEvent` + `renderTriggers` (`agent/planner.go:158-215`) |
| Retry suppression | `EXISTS BUT PARTIALLY INTEGRATED` | engine retry cap (`server/engine.go:1030-1046`); prompt-level "禁止重试" (`planner.go:326`); no intent-level dedup |
| Loop detection | `NOT IMPLEMENTED` | only debounce, terminal-status guard, heartbeat (`server/engine.go:809-858`) |
| Budget enforcement | `EXISTS BUT PARTIALLY INTEGRATED` | per-run `MaxTurns`, task timeout, benchmark `MaxIntents`; **no task-level intent budget** |
| Task cancellation | `EXISTS AND INTEGRATED` | `kill_work`, `SoftDeleteIntent` (`db/exploration.go:389`), pause/resume (`server/engine.go:1068-1098`) |
| Guard hooks | `EXISTS BUT PARTIALLY INTEGRATED` | worker + chat only; planner missing (§A1.5) |
| Structured plan output | `EXISTS BUT PARTIALLY INTEGRATED` | batch `add_intent` returns `{ids, errors}` (`agent/tools.go:1140-1144`); no plan-level contract |

No feature in the table is a candidate for wholesale replacement. The missing pieces are additive.

---

## A4. Baseline behaviour

### A4.1 Benchmark coverage gap

The shipped suite registry is `DefaultSuites` in `benchmark/dataset.go:21`, containing `artex-v5-offline-baseline` plus the IX.2.1 `artex-v5-dedup-v921` suite. **Neither contains a planning case.** Case categories are restricted to `detection_positive | detection_negative | lifecycle_edge` (`benchmark/case.go:99`), and `RunCase` always seeds intents directly (`benchmark/runner.go:118-127`) — the planner is never the measured surface.

Classification: **`BENCHMARK GAP DOCUMENTED`** (not invented).

### A4.2 Baseline defect reproduction (to be executed before remediation)

The following scenarios were executed against the pre-change code on a disposable PostgreSQL database (`artex_ix222`, migration role `artex_mig_ix222`, runtime role `artex_runtime_ix222`):

| Scenario | Method | Observed |
|---|---|---|
| S1 — same intent proposed twice sequentially | `AddIntent` ×2 with identical payload/assets | **2 rows** (no dedup) |
| S2 — equivalent intent, different wording | `AddIntent` ×2, summaries differ only in case/whitespace | **2 rows** |
| S3 — 10 concurrent proposals of the same intent | goroutines + barrier | **10 rows** |
| S4 — dependency declared | `add_intent` has no dependency field | field absent → `NOT IMPLEMENTED` |
| S5 — invalid intent | empty summary / bad parent kind | rejected by `addOneIntent` only |

Exact commands, exit codes and row counts are recorded in the implementation report (§6), because this gate is read-only and the lab is shared with the remediation gates.

---

## A5. Findings that constrain the design

1. **Intent dedup belongs at `addOneIntent` + `AddIntent`**, the single write path — mirroring the IX.2.1 decision to enforce finding identity at `RecordFindingTx`.
2. **No DB uniqueness constraint is safe here.** Intent states are mutable (`open → running → done →` retest), and retests are legitimate. A unique index would either block retests or require destructive state modelling. Gate B4 explicitly defers DB uniqueness until the identity contract is reviewed; this audit recommends **transaction-scoped application enforcement**.
3. **Scope is the exploration id.** `exploration_nodes.exploration_id` already isolates tasks; no cross-task dedup is possible or desirable.
4. **Only two structured discriminators exist** (`asset_ids` and `priority`); everything else is free text. A deterministic identity therefore needs at least one structured field beyond text, or it degenerates to string equality.
5. **The planner has no Guard hooks.** Any phase that claims "Planner cannot bypass policy" must either wire `guard.Hooks()` into `Plan` or state the limitation explicitly.
6. **No task-level intent budget exists.** The only hard stops are per-run turns and task timeout; a planner can create new distinct intents indefinitely. This is the loop-control gap.
7. **Dependencies are provenance-only.** `derived_from` records *why* an intent exists, not *when* it may run. `Frontier`/`ClaimIntent` ignore edges entirely.

---

## A6. Gate A conclusion

- Planner is a prompt-driven tool-calling agent; **no plan object or plan validation stage exists**.
- Deduplication is **prompt-only** and unverifiable.
- Dependency handling, loop detection and task-level budgets are **absent**.
- Guard policy is **not applied to planner tool calls**.
- The benchmark has **no planning cases**; the gap is documented rather than papered over.
- The existing architecture (single `AddIntent` write path, explicit intent states, engine-owned retries, trigger-driven feedback) is sufficient to host the improvements additively.

**No application source was modified during Gate A.**
