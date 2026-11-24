# PHASE8_3_5 — PAIRED PILOT RESULTS

One paired run. **Exploratory evidence only** — not statistical proof of superiority.

## Conditions (identical for both)

| Condition | Value |
|---|---|
| Provider / model | `openai` compat → `nvidia/nemotron-3.5-lightning-30b-a3b` @ `https://integrate.api.nvidia.com/v1` |
| Rate limit | 0.3 req/s, 18 req/min |
| Task text | identical (see `PHASE8_3_5_PILOT_CASE.md`) |
| `timeout_seconds` | 1200 |
| Fixture | `bench-vuln.py` @ `127.0.0.1:3004`, hits log truncated immediately before each run |
| Scope | only `127.0.0.1:3004` (task text; v5 also enforces at transport level) |
| DB | isolated per version, both freshly created |
| Run order | baseline first → reset → modified |

## Baseline run (task id 2, `artex_base_pilot`)

| Measurement | Value |
|---|---|
| Start → end | 20:47:53 → 20:51:13 (~200 s) |
| Task status | `done` |
| LLM calls | **15** (goals 2, planner 7, worker 6) |
| Tokens (in/out) | 217,838 / 15,009 |
| HTTP requests to fixture | **3** |
| Endpoints touched | 1 (`/search`) |
| Parameters touched | 1 (`q`) |
| Authenticated endpoints | 0 |
| Findings | **1** |
| Finding | `sql_injection` — "SQL Injection (Boolean-Based) via GET /search?q=", high, `pending` |
| Evidence | `GET /search?q=1' AND 1=1-- → 200, 56 products` / `GET /search?q=1' AND 1=2-- → 200, empty array` |
| Ground-truth match | yes (id 1, `sqli_boolean`) |
| Evidence authenticity | both requests present in `bench-hits.log`; 56 vs 0 reproduced independently |
| Out-of-scope traffic | none |
| Provider / app errors | none |
| Classification | **VALID DETECTION** |

Raw hits:
```
{"method":"GET","path":"/search?q=true","cookie":false}
{"method":"GET","path":"/search?q=1%27+AND+1%3D1--","cookie":false}
{"method":"GET","path":"/search?q=1%27+AND+1%3D2--","cookie":false}
```

## Modified v5 run (task id 2, `artex_pilot`)

| Measurement | Value |
|---|---|
| Start → end | 20:51:43 → 20:54:17 (~160 s) |
| Task status | `done` |
| LLM calls | **23** (goals 4, planner 5, worker 14) |
| Tokens (in/out) | 361,005 / 13,624 |
| HTTP requests to fixture | **4** |
| Endpoints touched | 2 (`/ok`, `/search`) |
| Parameters touched | 1 (`q`) |
| Authenticated endpoints | 0 |
| Findings | **1** |
| Finding | `sql_injection` — "SQL Injection (Boolean-Based)", high, `pending` |
| Evidence | `True payload (' OR 1=1--): returns 56 products; False payload (' OR 1=2--): returns empty set. Baseline q=test returns 3 products.` |
| Ground-truth match | yes (id 1, `sqli_boolean`) |
| Evidence authenticity | both requests present in `bench-hits.log`; differential reproduced |
| Out-of-scope traffic | none (scope guard active, no blocked attempts logged) |
| Provider / app errors | none |
| Classification | **VALID DETECTION** |

Raw hits:
```
{"method":"GET","path":"/ok","cookie":false}
{"method":"GET","path":"/search?q=test","cookie":false}
{"method":"GET","path":"/search?q='%20OR%201=1--","cookie":false}
{"method":"GET","path":"/search?q='%20OR%201=2--","cookie":false}
```

## Comparison table

| Measurement | Baseline | Modified v5 |
|---|---|---|
| Real Planner inference | yes | yes |
| Real Worker inference | yes | yes |
| Provider identity verified | yes (config + logs) | yes (config + logs) |
| Model identity verified | yes (`llm_usage`) | yes (`llm_usage`) |
| Selected tools | WebFetch/Bash (implicit) | HTTPRequest path (implicit; `Probe` oracle not selected) |
| HTTP requests | 3 | 4 |
| Valid finding | yes | yes |
| Evidence quality | genuine request/response differential | genuine request/response differential + explicit negative baseline |
| Runtime | ~200 s | ~160 s |
| LLM calls | 15 | 23 |
| Input tokens | 217,838 | 361,005 |
| Output tokens | 15,009 | 13,624 |
| Provider errors | none | none |
| Application errors | none | none |

## Scoring (pilot, single positive case)

| Metric | Baseline | Modified v5 |
|---|---|---|
| TP | 1 | 1 |
| FP | 0 | 0 |
| FN | 0 | 0 |
| TN | not measured (no negative-case task) | not measured |
| Precision | 1/1 = 1.00 | 1/1 = 1.00 |
| Recall | 1/1 = 1.00 | 1/1 = 1.00 |
| F1 | 1.00 | 1.00 |

Zero-denominator note: TN is **not** reported as a number because the pilot did not task either version with negative-only endpoints. Precision/recall each have denominator 1, so they are arithmetic facts about this single case only.

## Interpretation

- **Detection outcome on this case: no measurable difference.** Both found the same SQLi with authentic evidence.
- **Efficiency: mixed.** Modified finished faster (160 s vs 200 s) but used more model calls (23 vs 15) and more input tokens (361k vs 218k). Baseline used fewer requests (3 vs 4).
- **Oracle advantage did not manifest.** v5's deterministic `Probe` oracle was available but the planner chose manual HTTP requests; the finding is LLM-reasoned in both versions.
- **Evidence:** both are real. Modified's evidence text additionally states the neutral baseline (`q=test → 3`), which is marginally clearer.
- **Caveats:** one run, one case, temperature default (not 0), model nondeterminism, and the two candidates are different upstream revisions. No superiority claim follows.
