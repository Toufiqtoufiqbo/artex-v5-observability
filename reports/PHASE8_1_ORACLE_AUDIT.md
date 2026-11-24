# Phase VIII.1 — PHASE8_1_ORACLE_AUDIT

Independent live validation of every Probe oracle against the live bench app (`bench-vuln.py` :3004). (Previously shipped as `PHASE8_1_BENCHMARK.md` oracle section; consolidated here for traceability.)

| Oracle | Verdict (live) | Key measurements |
|---|---|---|
| `sqli_boolean` | **CONFIRMED** | baseline=124 B / true=1861 B / false=124 B; distinct_responses=1/1/1 |
| `sqli_time` | **NOT CONFIRMED** (negative control) | baseline=2ms; SLEEP(3) payloads: 2/5/7/2ms — no delay > threshold 2.5s (correct) |
| `xss_reflect` | **CONFIRMED** | raw reflection of `"><svg/onload=artexNNN>`; status 200 |
| `ssti_eval` | **CONFIRMED** | `${7*7}` → "49" in body (baseline without 49) |
| `path_traversal` | **CONFIRMED** | `root:x:0:0` signature from `../../etc/passwd` (canary) |

Unit tests also pass (`TestProbeSQLiBoolean`, `TestProbeSQLiBooleanNegative`, `TestProbeXSSReflect`, `TestProbeSSTI`, `TestProbeTraversal`, `TestIdorDiff`, `TestIdorDiffSame`, `TestFuzz`).

Limitations: XSS execution verification is browser-lab only (by design); time-based oracle validated on one app (no SLEEP vuln).
