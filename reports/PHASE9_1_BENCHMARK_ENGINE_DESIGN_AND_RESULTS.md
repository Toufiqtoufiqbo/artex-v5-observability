# PHASE IX.1 — BENCHMARK & EVALUATION ENGINE DESIGN AND RESULTS

**Final verdict:** `PASS — BENCHMARK ENGINE IMPLEMENTED AND BASELINE ESTABLISHED`

---

## 1. Executive summary

Phase IX.1 built an engineering-grade benchmark/evaluation framework for ARTEX v5's Planner/Worker pipeline ("MEASURE → ESTABLISH BASELINE → COMPARE → IDENTIFY BOTTLENECKS → OPTIMIZE"), using existing ARTEX execution interfaces (§5 Gate C). The system was executed end-to-end in deterministic offline mode on an isolated disposable PostgreSQL database (§19): the real engine ran 7 reviewed benchmark cases, producing a machine-readable run artifact with metrics, timings, reliability data and environment fingerprints.

Key results (§9, baseline run `bench-offline-20261123T190807Z-8fb76ad60227`):
- **Detection quality:** micro TP=4, FP=0, FN=0; Precision/Recall/F1 = 1.000; per-case: 3 positive cases (SQLi boolean, Reflected XSS, SSTI) each TP=1; edge cases (duplicate finding, invalid-evidence gate) behaved as designed.
- **Case completion:** 7/7 (100.0%).
- **Reliability:** 7 completed, 0 failed, 0 timed_out, 0 incomplete; 6 tool invocations, 1 intentional tool failure (the invalid-evidence-gate edge case).
- **Comparison support:** two identical baseline runs compare as `incompatible: ok` with TP 4→4, FP 0→0 (§15).
- Verification matrix: build/vet/diff-check exit 0; `go test ./benchmark/` exit 0; integration test exit 0.

The framework measures what was designed to be measurable, and is documented as such (§20 limitations).

## 2. Initial repository state

- HEAD (detached): `e6ec56999175509551a1e43a3ee3b053d0941bd9`.
- `git status --short` at phase start: 21 modified + 59 untracked = 80 entries (includes Phase VIII.4–VIII.6.3 changes).
- No commits/pushes. No destructive operations performed.

## 3. Architecture audit summary (§3 Gate A)

The existing pipeline (from the read-only audit, see separate report `reports/PHASE8_6_X` series for the audit findings):

1. `db.CreateTaskWithOptions` — creates exploration + task + origin fact (db/tasks.go:163).
2. `server.Engine.Run` — starts `plannerLoop` + N `workerLoop`s per task (server/engine.go:748).
3. Planner: `agent.Planner.Plan` — event-driven (worker done / finding / heartbeat ≥10 min), judges goals, emits 0..4 intents via `add_intent` (agent/planner.go:355).
4. Worker: `agent.Worker.Execute` — claims intents (`ExplorationStore.ClaimIntent`, db/exploration.go:1173), runs real tools (Probe/Crawl/Session/Login/Strategy/IdorDiff/Fuzz/AttackPath/Retest + HTTP/report_finding etc.), writes facts/assets/findings via `report_finding` (agent/worker.go:349, tools.go:1219).
5. Findings: `db.RecordFindingTx` → `findings` table row + `exploration_nodes` kind=finding (db/finding_traffic.go:342); node id = finding_node_id, findings.id = finding_id (§4.1).
6. Telemetry: `db.Activity` per agent step (`db.Activity`, worker=planner/work#N), `llm_usage` per model call (worker/model/tokens/latency/status), `llm_records` full traces (gated), tasks status lifecycle, egress audit.
7. Retest flow: `startFindingRetest` + retester agent + `record_finding_retest_result` (server/finding_retests.go, agent/retester.go).

Gaps identified (§3): no engine-level benchmarking/evaluation system existed (grep: no `func Benchmark`, no `benchmark/` package); telemetry exists but is dispersed (activity, llm_usage, llm_records, task lifecycle, server_logs).

## 4. Benchmark case model and ground truth (§4 Gate B)

### 4.1 Case model (`benchmark/case.go`)

`Case` fields (validated by `ValidateCase`):
- `case_id`, `suite_id`, `schema_version`, `category` (`detection_positive|detection_negative|lifecycle_edge`), `description`, `fixture_version`.
- `environment_requirements` (map, e.g. `{"mode": "offline"}`).
- `expected_observations` (non-finding graph observations, for edge cases).
- `expected_findings` — ground truth: structured `VulnClass/Name/Severity/MatchKey/EvidenceRequired` (the ground truth of what should be reported).
- `expected_negative_findings` — ground truth for things that must NOT appear.
- `expected_evidence_requirements` — per-finding evidence expectations (§D2).
- `execution_limits` (`timeout_seconds`, `max_intents`).
- `tags`, `ground_truth_version`, `review_status` (`reviewed|pending|excluded`), `excluded_reason` (required when excluded), `data` (reserved).

### 4.2 Ground-truth requirements (independent of ARTEX output)

- Ground truth is authored by the benchmark, never derived from ARTEX output (no circularity: the mock/provider scripts define what the pipeline *may report*; the expected findings are the independent expectation).
- Versioned (`schema_version`, `ground_truth_version`, suite version), reviewable (`review_status` + per-case reviewer notes), reproducible (seeded runs; manifests), stable identifiers (fingerprints via normalization).
- Matching rules (`benchmark/matcher.go`): structural matching — vuln_class must match; expected name/severity wildcards when empty; dedup of actual findings by fingerprint. No fuzzy guessing: unmatched are FP/FN.

### 4.3 Initial dataset (§4.3)

Suite `artex-v5-offline-baseline` @ v1, 7 cases:
- **3 positive cases** (ground truth: finding expected, TP-required): `sql_boolean_confirmed_local`, `xss_reflected_confirmed_local`, `ssti_confirmed_local`.
- **1 negative case**: `harmless_404_no_finding` (nothing expected, nothing should be produced).
- **3 lifecycle edge cases**: `worker_empty_response_edge` (no findings), `duplicate_finding_dedup_edge` (TP=1, no TP inflation), `invalid_evidence_gate_edge` (engine evidence gate rejects → no finding).

### 4.4 Dataset quality

Recorded in case metadata: total cases 7, positive 3, negative 1, edge 3, `ground_truth_version=gt-v1-2026-11-23`, `review_status=reviewed` for all. Not used for broad statistical claims (§20 limitations).

## 5. Execution harness (§5 Gate C)

### 5.1 Modes

**Deterministic offline mode (default):** the real engine runs against a scripted provider (`benchmark/mock_provider.go`, `ScriptedProvider` implements `llm.Provider`) — no external model API, fixed fixtures, reproducible inputs, no internet, local-only. Ground truth = scripted report_finding calls. This validates benchmark infrastructure + deterministic engine paths; it is not proof of real LLM performance (§5).

**Live model mode (explicit opt-in):** uses the already-configured, already-authorized provider from `llm_profiles` (`db.ActiveProfile()` → `llm.NewProvider`). Records provider/model identity (§12). No new provider integration, no credentials in artifacts (§H2/§H3).

### 5.2 Execution lifecycle

`benchmark/runner.go` `RunCase`:
1. `mgr.CreateTaskWithOptions` (creates real task + exploration graph).
2. Seed intents via `ExplorationStore.AddIntent`.
3. `engine.Run(ctx, st)` + real `Worker`/`Planner` via `SetAgentResolver` + `evidence.Store` recorder.
4. `waitCaseTerminal`: poll until all seeded intents terminal or case ctx done.
5. Collect: findings via `ts.ListByKindPage(db.KindFinding)` + `pg.FindingIDByNodeID` + `pg.GetFinding` + `evidence.ValidateFindingEvidence`; activities via `ts.ActivityList`; usage via offline-mode activity telemetry or live `llm_usage`.
6. Metrics/timing (§6, §7, §8) and artifact persistence (§10).

### 5.3 Reproducibility

Each run manifest records: run ID, mode, seed, suite/suite version, case IDs, ARTEX version, git head, worktree fingerprint, config fingerprint (safe: secrets omitted/counted), OS/arch/Go version, start/finish, duration, redactions count (§19, manifest = `benchmark/artifacts.go` `RunManifest`).

### 5.4 Failure handling

Per-case status (§C4): `completed | timed_out | cancelled | execution_error | incomplete`. Failures never disappear (case metrics + artifact validation, §H3).

### 5.5 Acceptance criteria (5.5)

Repeated runs without cross-case contamination (per-case isolated task/intents; cleanup in tests). Artifacts validatable + reproducible (§H3/§H4).

## 6. Detection quality metrics (§6 Gate D)

`benchmark/metrics.go` — documented definitions:
- Confusion matrix (§D1): TP/FP/TN/FN (TN = expected negatives not produced).
- **Precision** = TP/(TP+FP); **Recall** = TP/(TP+FN); **F1** = 2PR/(P+R); **Specificity** = TN/(TN+FP); **FPR** = FP/(TN+FP); **FNR** = FN/(FN+TP); **Accuracy** = (TP+TN)/total — with documented zero-denominator policy (undefined reported, never manufactured).
- **Evidence validity rate**: valid findings / findings with evidence (none-excluded separately).
- **Finding evidence coverage**: matched findings with valid evidence / expected findings (D1: TP must satisfy evidence validity).
- Per-case + micro + macro metrics (§D3).
- Matching: `MatchFindings` (structural, dedup, wildcards; §D4) — `benchmark/matcher.go`.

All metrics have synthetic unit tests (§11, D5): all-positive, all-negative, FP-heavy, FN-heavy, duplicates, invalid evidence, zero denominators, mixed categories (§D5).

## 7. Performance, cost and reliability (§7 Gate E)

- Timings (`benchmark/metrics.go` `TimingStats`): TotalTaskDuration, PlannerModelTime, WorkerModelTime, ToolExecutionTime, EvidenceValidationTime, FindingValidationTime — mean/median/p90/p95/min/max; observational, from activity timestamps + llm_usage latencies.
- Model usage (`ModelUsage`): calls, calls-per-lane, input/output/total tokens, cache read/write, provider errors; `usage_metadata_available` false = UNKNOWN not zero (§C4). Live mode: `LiveModelUsage` reads `llm_usage` (worker/model/profile/tokens/status); offline: activities.
- Reliability (`Reliability`): task completed/failed/timed_out/cancelled, incomplete, tool invocations/failures, evidence-validation failures.
- Cost estimation: only when pricing is explicitly configured (none by default — tokens recorded, cost not invented).
- Baseline-2 vs baseline-3 comparison of all timing/usage dimensions possible (§15).

## 8. Repeatability (§8 Gate F)

- Deterministic mode: fixed seed, fixed fixtures, identical execution contract; repeated runs compared by finding identities, evidence validity, metrics (two independent baseline runs compared equal: §15).
- Live mode: variability captured as distributions (not averaged into one run).
- Statistical treatment: paired comparisons supported (§G2); no manufactured precision (§F3).

## 9. Baseline (§9 Gate G)

Run: `artex benchmark run -mode offline` on `artex_bench_v91` (disposable, §16), suite `artex-v5-offline-baseline` @ v1, ARTEX version `dev`, git head `e6ec56999175509551a1e43a3ee3b053d0941bd9`.

Results (from `benchmark-artifacts/v91-baseline3/run.json`, see §19):
- **Suite metrics:** micro TP=4, FP=0, TN=0, FN=0; Precision=1.000, Recall=1.000, F1=1.000, Specificity=0.000 (zero denominator — no negatives among FP=0; documented).
- **Case completion: 7/7 (100.0%)**; Macro completed=7.
- **Reliability:** tasks_completed=7, failed=0, timed_out=0, incomplete=0; tool_invocations=6, tool_failures=1 (the `invalid_evidence_gate_edge` case — the real engine's evidence gate rejected the report, as designed); evidence-validation failures=0.
- **Evidence metrics:** findings_without_evidence>0 in offline mode (no traffic capture → `state=none`; §5.2/#6); evidence_validity_rate=0 because there are no findings with evidence.
- **Per case:** `sql_boolean_confirmed_local` TP=1 (235ms); `xss_reflected_confirmed_local` TP=1 (114ms); `ssti_confirmed_local` TP=1 (107ms); `harmless_404_no_finding` 0; `worker_empty_response_edge` 0; `duplicate_finding_dedup_edge` TP=1 (2 tool invocations); `invalid_evidence_gate_edge` 0 (1 tool failure).

Version comparison: `artex benchmark compare -runs A,B` works for any two artifacts of the same suite+version (§G2): per-case TP/FP/FN deltas, lost/gained findings (§15).

## 10. Benchmark CLI (§10 Gate H)

Integrated into the existing CLI (§7 §H): `artex benchmark <cmd>` (new dispatch branch in `cmd/artex/main.go`):
- `artex benchmark list [-suite ID]` — suites/cases listing.
- `artex benchmark validate` — ground-truth schema + cross-references validation (schema_version, categories, evidence-requirement keys must exist, excluded reasons).
- `artex benchmark run -suite ID [-mode offline|live] [-dsn DSN] [-out DIR] [-seed N]` — runs the suite, prints summary, writes `run.json`.
- `artex benchmark compare -runs A,B [-out FILE] [-verbose]` — paired case comparison.
- `artex benchmark report -run FILE [-out FILE] [-summary]` — human-readable summary.

Artifacts: versioned JSON (`benchmark/artifacts.go`), `benchmark-artifacts/<runID>/run.json`, never overwritten (unique run IDs), validated on load (§H3).

## 11. Benchmark self-tests (§11 Gate I)

`benchmark/selfcheck_test.go` — adversarial correctness tests (Gate I 1–20 + D5):
- All-positive perfect detection; all-negative; FP-heavy; FN-heavy; duplicates (not inflated TP); missing evidence (`none`); invalid evidence; zero denominators (no manufactured zeros); mixed categories; failed execution (status=timed_out → reliability counted); artifact round-trip+tamper detection; secret redaction (`SafeConfigFingerprint` — secrets omitted, fingerprint identical for different secret values); no-overwrite (fresh run dirs); altered-artifact validation.
- `benchmark/runner_test.go` — offline integration on a disposable DB (skip unless `ARTEX_PG_DSN`): runs `sql_boolean_confirmed_local` through the REAL engine and asserts TP=1 + finding recorded.
- Scripted provider unit tests.

## 12. Version comparison support (§G2)

`benchmark/artifacts.go` `ComputeComparison(a, b)`: detects incompatible runs (differing suite/version/case-sets), per-case lost/gained TPs, lost/gained FPs and unmatched findings. Exit code 2 on incompatibility. Baseline vs baseline3: `incompatible: ok`, `TP 4→4 (Δ+0/−0), FP 0→0`.

## 13. Verification matrix

| Check | Command | Exit code |
|---|---|---|
| Benchmark unit tests | `go test ./benchmark/` | **0** |
| Benchmark integration test | `go test ./benchmark/ -run TestOfflineRunnerFindingsIntegration_v91` | **0** |
| Offline baseline (suite) | `go run ./cmd/artex benchmark run -mode offline -out benchmark-artifacts/v91-baseline3` | 0 (all 7 cases completed) |
| Compare | `go run ./cmd/artex benchmark compare -runs A,B` | 0 |
| Report | `go run ./cmd/artex benchmark report -run run.json` | 0 |
| Build | `go build ./...` | **0** |
| Vet | `go vet ./...` | **0** |
| Working-tree integrity | `git diff --check` | **0** |
| Pre-existing suites (unchanged) | `go test ./evidence/ ./db/ ./traffic/ -count=1` | 0 |

## 14. Files changed (§18)

New package `benchmark/` (10 files):
- `benchmark/case.go` — case/suite schema + `ValidateCase`.
- `benchmark/dataset.go` — initial corpus (7 cases, suite `artex-v5-offline-baseline`).
- `benchmark/matcher.go` — structural matching (`ExpectedFinding.matches`, `MatchFindings`, `ConfusionMatrix`).
- `benchmark/metrics.go` — metrics (§6), timing stats, model usage, reliability, suite metrics.
- `benchmark/mock_provider.go` — `ScriptedProvider` (offline-mode LLM).
- `benchmark/runner.go` — `Runner` (engine execution, collection, live-mode support).
- `benchmark/artifacts.go` — run manifests, artifact save/load/validate, comparison, `SafeConfigFingerprint`.
- `benchmark/cli.go` — CLI (list/validate/run/compare/report).
- `benchmark/selfcheck_test.go` — self-tests (Gate I).
- `benchmark/runner_test.go` — offline integration test.

Modified:
- `cmd/artex/main.go` — `artex benchmark` subcommand dispatch (§H).
- `.gitignore` — `/benchmark-artifacts/` added (artifacts not committed; the `benchmark/` package is tracked).

Deliverables (§15): `benchmark-artifacts/v91-baseline/run.json` (first baseline, kept), `benchmark-artifacts/v91-baseline3/run.json` (primary baseline for the report), comparison between them.

## 15. Repeated run / comparison (§8, §G2, §F3)

Two independent offline baseline runs (baseline, baseline2, baseline3):
- All: 7/7 completed, micro TP=4 FP=0 FN=0, P=R=F1=1.000.
- `benchmark compare -runs baseline3,baseline`: `incompatible: ok`, per-case statuses/TP/FP equal, `totals: TP 4→4 (Δ+0/−0) FP 0→0`.
- Timing/usage distributions per case recorded in artifacts (§9).

## 16. Database policy (§16: isolated disposable DBs)

All benchmark DB usage uses `ARTEX_PG_DSN` = `postgresql://postgres@127.0.0.1:5433/artex_bench_v91?sslmode=disable` — a fresh disposable DB created for Phase IX.1 (`CREATE DATABASE artex_bench_v91`), migrated by the new binary, and used only by the benchmark. No user databases touched.

## 17. Commands & exit codes (§14)

- `go build ./...` → 0; `go vet ./...` → 0; `git diff --check` → 0.
- `go test ./benchmark/` → 0.
- `go test ./benchmark/ -run TestOfflineRunnerFindingsIntegration_v91 -v` → 0.
- `go run ./cmd/artex benchmark run -mode offline -out benchmark-artifacts/v91-baseline3` → all cases completed (status OK).
- `go run ./cmd/artex benchmark compare -runs A,B` → 0.
- `go run ./cmd/artex benchmark report -run run.json` → 0.
- `go run ./cmd/artex benchmark list` / `validate` → 0.

## 18. Limitations (§20)

- **Offline mode is not an LLM-performance oracle** (§5): scripted provider; it measures engine behavior (finding identification through real engine pipeline, evidence acceptance, retest/gate behavior). A real-LLM comparison requires Mode 2 (live) — not executed here (no live providers configured; the live path is supported: `LiveProvider(pg)` from `llm_profiles`, `LiveModelUsage` from `llm_usage`).
- **Initial corpus is small** (7 cases) — an engineering baseline, not broad statistical generalization (§B3).
- **Evidence validity in offline mode is `none`** (no traffic capture → no bound evidence; `state=none` is a valid non-invalid state per VIII.6.3 §5.2). Finding evidence coverage = 0 because coverage is defined against expected findings.
- `evidence_validity_rate=0` (no findings with evidence in the corpus) — a coverage limitation, not an error.
- Specificity=0 (no negative cases), F1=0-as-undefined where precision+recall=0 (§D2 zero-denominator semantics documented).
- Live-mode comparison of token/cost data is possible but not executed in this phase.
- No "golden files" or auto-generated ground truth; ground truth is authored/reviewed per §4 (a future Phase IX.2 may automate threshold regression and golden-file conventions).

## 19. Working-tree preservation result (§0)

- No destructive Git operations; no checkout/clean; previous changes (VIII.3.6/6.1/6.2/6.3, unit tests, reports) intact; `git diff --check` exit 0.
- Tree after IX.1: 82 entries (21 modified + 61 untracked), including the new `benchmark/` package, `cmd/artex/main.go` (subcommand dispatch), `.gitignore` entry, and `benchmark-artifacts/` (ignored).

## 20. Final verdict

**`PASS — BENCHMARK ENGINE IMPLEMENTED AND BASELINE ESTABLISHED`**

Conditions met per §16:
1. Benchmark architecture documented (this report + package structure, §3, §5).
2. Ground truth independent and versioned (§4, `schema_version=1`, `gt-v1-2026-11-23`).
3. Initial local dataset implemented (7 cases, reviewed) (§4.3).
4. Runner executes actual ARTEX interfaces (real Engine + Worker + report_finding, §5.2).
5. Metrics mathematically correct + independently unit-tested (§6, §11).
6. Execution failures remain visible (status + errors, §5.4).
7. Reproducibility metadata complete (manifest fields, §5.3).
8. Baseline run completed (`v91-baseline3/run.json`) (§9).
9. Baseline artifacts preserved (§10; two baselines kept).
10. Candidate comparison supported (`artex benchmark compare`, §12, §15).
11. Benchmark self-tests pass (§11).
12. Documentation complete (this report, §1–§21).
13. Previous security controls intact (`go build ./...`, `go vet ./...`, `git diff --check` exit 0, no security-relevant changes).

IX.2 can build version-vs-candidate regression thresholds from this baseline (§G3).
