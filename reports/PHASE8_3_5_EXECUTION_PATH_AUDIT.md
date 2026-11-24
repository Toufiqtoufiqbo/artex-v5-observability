# PHASE8_3_5 — EXECUTION PATH AUDIT

Both trees inspected at source level. `server/task_llm.go` is **line-for-line identical** in both (same function names, same line numbers), which is the strongest structural parity signal in this phase.

## 1. Traced path

```
POST /api/tasks                       server/server.go (baseline:1572, modified:1462)
  └─ createTask → launchTask          server/goals.go:28
       └─ engine.Run(ctx, task)       server/goals.go:65
            └─ agentsForTask(t)       server/task_llm.go:552
                 ├─ goalRuntime       agentKey "goals"      (task_llm.go:558)
                 ├─ plannerRuntime    agentKey "planner"    (task_llm.go:559)
                 ├─ workerRuntime     agentKey "worker"     (task_llm.go:560)
                 └─ mainRuntime       agentKey "mainagent"  (task_llm.go:561)
                 ├─ agent.NewWorker(workerRuntime, ...)     (task_llm.go:564)
                 └─ agent.NewPlanner(plannerRuntime, ...)   (task_llm.go:574)
                      └─ taskLLMRuntime.Stream / Complete   (task_llm.go:222 / 232)
                           └─ current()                     (task_llm.go:110)
```

## 2. Provider resolution (the actual decision point)

`taskLLMRuntime.current()` (task_llm.go:110) resolves in this precedence:

1. **Agent binding** — `agentBindingProvider(agentKey)` (task_llm.go:494) reads `agents.llm_profile_id`.
2. **Task profile chain** — `tasks.LLMProfileIDs` / `ActiveLLMProfileID`.
3. **Global provider** — `globalProvider()` (task_llm.go:509) returns `s.llmProv`, `s.llmCfg`.

Verified at runtime for this phase:

```
SELECT key,llm_profile_id FROM agents WHERE llm_profile_id IS NOT NULL;
-- baseline artex_base_pilot: 0 rows
-- modified artex_pilot:     0 rows
```

Task was created **without** `llm_profile_ids`, so `len(pt.LLMProfileIDs)==0` → branch 3 → **global provider** = the NVIDIA profile set via `POST /api/llm`.

`globalProvider()` source (identical in both):

```go
func (s *Server) globalProvider() (llm.Provider, agent.Config, bool) {
    s.cfgMu.Lock(); defer s.cfgMu.Unlock()
    if !s.llmOn || s.llmProv == nil { return nil, agent.Config{}, false }
    return s.llmProv, s.llmCfg, true
}
```

## 3. Answers to the required questions

| Question | Finding |
|---|---|
| Provider consumed by Planner | global provider (no binding, no task chain) |
| Provider consumed by Workers | same global provider (`workerRuntime`) |
| Separate Planner/Worker model configs | mechanism exists (per-agent binding) but **no bindings set** → both use the same model |
| Conversation-specific profile in this path | **no** — the `claude-x` profile is only used by `POST /api/llm/test` (side-question), not by Planner/Worker |
| Agent-level override of model | none present in either DB |
| Fallback provider | none active; `current()` returns an error if nothing resolves rather than silently picking a different model |
| Retry on failed call | yes, same-provider retry policy `sameProviderRetryPolicy` (task_llm.go:414), identical both sides |
| Model responses observable | yes — `llm_usage` table (task_id, worker, model, latency_ms, input/output tokens, status) |
| Configured base URL actually used | yes — provider built from `s.llmCfg` (`[engine] LLM configured via UI: openai / nvidia/...`) |

## 4. Tool surface (what the Worker can call)

The orchestration/selection mechanism is the same, but the **tool catalog differs** — this is the evaluated product difference, not a defect:

| Capability | Baseline | Modified v5 |
|---|---|---|
| Generic HTTP fetch | `WebFetch` / `Bash` | `HTTPRequest` (single scoped transport) |
| Deterministic oracles | — | `Probe` (sqli_boolean, sqli_time, xss_reflect, ssti_eval, path_traversal) |
| Crawl | — | `Crawl` |
| Session/login | — | `Login`, `Session` |
| IDOR diff | — | `IdorDiff` |
| AttackPath / Retest | — | `AttackPath`, `Retest` |
| Scope guard | — | `SetTaskScope` / `CheckScope` |

Both versions expose tools to the **same** LLM planner/worker loop; only the catalog differs. No prompt or schema was modified in this phase.

## 5. Conclusion

Both versions run the identical resolution code and were configured with the identical global provider. Any behavioural difference observed in the pilot therefore originates in **tool availability + orchestration decisions**, not in provider wiring.
