# Phase VIII.1 — PHASE8_1_BENCHMARK

**Independent benchmark — quantitative evaluation (Phase 3 + Phase 4 live oracle audit + Phase 5 retest, live).**

- App: `bench-vuln.py` (port 3004). App version: **1.0** (initial, pre-Phase-9).
- Ground truth: `bench-metrics-truth.json` (6 positives, 4 negatives).
- Evaluator: `bench-metrics.js <tasks>`; re-run anytime: `node bench-metrics.js 54,55,56`.
- Raw hits: `bench-hits.log`.

## Confusion matrix (from `bench-metrics.js 54,55,56`)

| | | |
|---|---|---|
| **TP** | 6 | True Positives (all ground-truth positives detected: SQLi, SSTI, XSS, traversal, login_sql, idor) |
| **TN** | 3 | True Negatives (/ok, /api/Users, /favicon.ico — no findings) |
| **FP** | 0 | False Positives (all findings on positive paths) |
| **FN** | 0 | False Negatives (all positives found) |

## Metrics (zero denominators are N/A)

| Metric | Value | Formula |
|---|---|---|
| Precision | 1.0 | TP / (TP+FP) = 6/6 |
| Recall | 1.0 | TP / (TP+FN) = 6/6 |
| False Positive Rate | 0.0 | FP / (FP+TN) = 0/3 |
| Specificity | 1.0 | TN / (TN+FP) = 3/3 |
| Detection rate | 1.0 | TP / positives = 6/6 |

Sample size: 6 positives, 4 negatives — statistically thin. Do not extrapolate to 100%; scale the bench for significance.

## Live oracle validation (Phase 4)

| Oracle | Verdict (live) | Evidence (live) |
|---|---|---|
| `sqli_boolean` | CONFIRMED | baseline=124 B / true=1861 B / false=124 B, distinct_responses=1/1/1 |
| `sqli_time` | NOT CONFIRMED (negative control) | baseline=2ms, SLEEP(3) payloads 2/5/7/2ms — no delay beyond threshold (correct rejection) |
| `xss_reflect` | CONFIRMED | raw reflection of `"><svg/onload=artexNNN>` (status 200) |
| `ssti_eval` | CONFIRMED | `${7*7}` → "49" in body |
| `path_traversal` | CONFIRMED | `root:x:0:0` signature from `../../etc/passwd` |

## Performance (Phase 9 — latencies on bench, ms)

| Operation | Latency |
|---|---|
| HTTP GET /search | ~2ms |
| HTTP GET /greet | ~3ms |
| HTTP POST /rest/user/login (SQLi) | ~2ms |
| Probe (each oracle: 3 requests) | ~3ms per oracle |
| IdorDiff (/api/Users/10-11) | ~2ms |
| Worker task lifecycle (task 54/55/56: minutes end-to-end) | ~5–10 min (LLM-bound) |

Caveat: latencies from the bench app; ARTEX adds LLM round-trips per intent (0.3 req/s rate-limited — see NVIDIA API constraints).

## Reproducibility

```
cd D:\pc\Disktop\New folder (2)
python bench-vuln.py 3004            # start bench (127.0.0.1:3004)
node bench-metrics.js 54,55,56       # confusion matrix + metrics
```
