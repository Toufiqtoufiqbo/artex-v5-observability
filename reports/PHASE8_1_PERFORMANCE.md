# Phase VIII.1 — PHASE8_1_PERFORMANCE

Latencies measured on the bench app (ms) via oracle outputs (`PHASE8_1_BENCHMARK.md`):

| Operation | Latency |
|---|---|
| HTTP GET /search | ~2 |
| HTTP GET /greet | ~3 |
| HTTP POST /rest/user/login (SQLi) | ~2 |
| Probe (each oracle: 3 requests) | ~3 |
| IdorDiff (/api/Users/10-11) | ~2 |

Worker lifecycle: tasks 54/55/56 timeline in `artex-v5b.log` (minutes-to-complete; LLM-bound, rate-limited by NVIDIA API: 0.3 req/s).

Caveats: latencies on the bench; ARTEX adds LLM round-trips; no retry loops; timeouts via worker clock.
