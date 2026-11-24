# PHASE8_3_5 — INFERENCE SMOKE

Goal: prove a **real** inference request through the main Planner path in each version — not just a saved config or a local HTTP 200.

Task used (identical text, harmless, no scanning):

```
description: "Inference smoke test. No target, no tools."
goal:        "Do not use any tools and do not make network requests. Reply with exactly the word OK."
timeout_seconds: 90
```

Command: `node phase835/smoke.js <port> ARTEX <pw> <label>`
Evidence source: `llm_usage` table (written by the engine per completed model call).

## 1. Baseline (:8888, `artex_base_pilot`)

```
id|task_id|worker|model                                |latency_ms|input_tokens|output_tokens|status
1 |1      |goals |nvidia/nemotron-3.5-lightning-30b-a3b |10309     |3387        |745          |ok
2 |1      |planner|nvidia/nemotron-3.5-lightning-30b-a3b|94926     |16176       |17958        |ok
3 |1      |planner|nvidia/nemotron-3.5-lightning-30b-a3b|14680     |16910       |2806         |ok
4 |1      |planner|nvidia/nemotron-3.5-lightning-30b-a3b|10467     |16318       |1917         |ok
```

Log: `[goals] task 1: LLM goal breakdown returned nothing` → `[planner] task 1 planning (trigger: edge)` → `[planner] task 1 planning finished`.
Task ended `timeout` because the first planner call took 95 s against a 90 s task deadline (configuration, not inference failure).

## 2. Modified v5 (:8787, `artex_pilot`)

```
id|task_id|worker|model                                |latency_ms|input_tokens|output_tokens|status
1 |1      |goals |nvidia/nemotron-3.5-lightning-30b-a3b |8444      |2869        |377          |ok
2 |1      |planner|nvidia/nemotron-3.5-lightning-30b-a3b|52312     |15528       |5728         |ok
3 |1      |planner|nvidia/nemotron-3.5-lightning-30b-a3b|20215     |21278       |1117         |ok
4 |1      |worker |nvidia/nemotron-3.5-lightning-30b-a3b|37095     |15040       |1885         |ok
5 |1      |planner|nvidia/nemotron-3.5-lightning-30b-a3b|21220     |22436       |471          |ok
```

Log: `[planner] task 1 规划中…(edge 触发)` → `[worker work#2] task 1 领取意图 #3` → `[planner] task 1 判定目标达成`.
Task ended `done`.

## 3. Gate A evaluation

| Requirement | Baseline | Modified v5 |
|---|---|---|
| Real request to configured provider | YES (`llm_usage` rows) | YES (`llm_usage` rows) |
| Intended provider (`openai` compat / NVIDIA) | YES (log: `LLM configured via UI: openai / nvidia/...`) | YES (same log line) |
| Intended model recorded | YES `nvidia/nemotron-3.5-lightning-30b-a3b` | YES, identical |
| Valid response received | YES (tokens, `status=ok`) | YES (tokens, `status=ok`) |
| Fallback provider activated | NO | NO |
| Mock used | NO | NO |

**Gate A: PASSED for both versions.**

Limitation: `llm_usage.model` is the model string the application associates with the call; the provider's own `response.model` echo is not surfaced by this table, and `llm_records` (raw request/response capture) was empty because raw recording is disabled. Model identity is therefore verified at the **application/provider-configuration** level plus successful token-bearing responses, not by provider-side request-ID cross-check.

Side note (not used as evidence): `POST /api/llm/test` still returns the unrelated `anthropic ... invalid x-api-key` from the `claude-x` side-question profile in both versions — identical behaviour, excluded by rule 7.
