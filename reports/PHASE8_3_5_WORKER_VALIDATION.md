# PHASE8_3_5 — WORKER VALIDATION

Question: does the Worker make independent model calls, and with which provider/model?

## 1. Architecture (identical in both trees)

`server/task_llm.go:558-564` (same line numbers in both):

```go
goalRuntime    := &taskLLMRuntime{s: s, taskID: t.ID, agentKey: "goals"}
plannerRuntime := &taskLLMRuntime{s: s, taskID: t.ID, agentKey: "planner"}
workerRuntime  := &taskLLMRuntime{s: s, taskID: t.ID, agentKey: "worker"}
mainRuntime    := &taskLLMRuntime{s: s, taskID: t.ID, agentKey: "mainagent"}
wk := agent.NewWorker(workerRuntime, "task-router", ...)
pl := agent.NewPlanner(plannerRuntime, "task-router", ...)
```

The Worker is constructed with its own `taskLLMRuntime`, so it calls `current()` independently. With no agent binding and no task profile chain, it resolves to the **same** `globalProvider()` as the Planner.

## 2. Observed runtime evidence

### Baseline (`artex_base_pilot`, pilot task 2)

```
id|worker |model                                |latency_ms|input|output|status
5 |goals  |nvidia/nemotron-3.5-lightning-30b-a3b|33628     |3472 |5918  |ok
6 |goals  |nvidia/nemotron-3.5-lightning-30b-a3b|9387      |9460 |1316  |ok
7 |planner|nvidia/nemotron-3.5-lightning-30b-a3b|11900     |16597|743   |ok
...
11|worker |nvidia/nemotron-3.5-lightning-30b-a3b|7741      |11584|408   |ok
12|worker |nvidia/nemotron-3.5-lightning-30b-a3b|4415      |12102|175   |ok
13|worker |nvidia/nemotron-3.5-lightning-30b-a3b|27988     |13182|129   |ok
14|worker |nvidia/nemotron-3.5-lightning-30b-a3b|47365     |13393|910   |ok
15|worker |nvidia/nemotron-3.5-lightning-30b-a3b|4400      |14405|343   |ok
17|worker |nvidia/nemotron-3.5-lightning-30b-a3b|11337     |14770|567   |ok
```

→ Baseline Worker **does** perform independent inference (6 worker calls), on the configured model.

### Modified v5 (`artex_pilot`, smoke task 1)

```
4 |worker |nvidia/nemotron-3.5-lightning-30b-a3b|37095|15040|1885|ok
```

→ Modified Worker **does** perform independent inference, same model.

## 3. Answers

| Question | Baseline | Modified v5 |
|---|---|---|
| Worker invokes the LLM | YES | YES |
| Provider/model used by Worker | global (NVIDIA model) | global (NVIDIA model) |
| Different model/profile for Worker | NO | NO |
| Tool selection performed by | Planner (intents) + Worker (tool calls) | same |
| Execution path reached | YES (fixture requests observed) | YES (fixture requests observed) |

Both architectures use a single shared model for planner and worker; no separate worker model was invented or assumed.
