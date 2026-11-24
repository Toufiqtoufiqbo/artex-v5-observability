# PHASE IX.2.2 — PLANNER INTENT MANAGEMENT & TASK DECOMPOSITION
## Results Report

**Generated:** 2026-11-24
**HEAD:** `e6ec56999175509551a1e43a3ee3b053d0941bd9` (detached)
**Working tree (recorded before edits, verified after):** 95 entries — 30 modified, 65 untracked; final diff `git diff --check` clean.
**Lab DB (disposable):** `artex_ix222` on `postgres://artex_runtime_ix222@127.0.0.1:55432/artex_ix222?sslmode=disable`; migration role `artex_mig_ix222` (DDL applied; applied schema = 49 tables); runtime role `artex_runtime_ix222` (DML only).
**Related phase reports:** `reports/PHASE9_2_2_PLANNER_FORENSIC_AUDIT.md` (Gate A), `reports/PHASE9_2_2_PLANNER_INTENT_IDENTITY_SPECIFICATION.md` (Gate B), and the Phase IX.2.1 reports in the same directory.

Evidence labels used throughout (per project convention): `SOURCE VERIFIED`, `CALL-PATH VERIFIED`, `TEST VERIFIED`, `INTEGRATION VERIFIED`, `RUNTIME VERIFIED`, `BENCHMARK VERIFIED`, `PARTIALLY VERIFIED`, `UNKNOWN`, `NOT INVESTIGATED`.

---

## 1. Objective
Implement PHASE IX.2.2 — Planner Intent Management & Task Decomposition for ARTEX v5: audit the existing Planner (Gate A), define deterministic intent identity (Gate B), prevent duplicate intents while preserving distinct ones, add dependency/validation/loop controls, direct Planner tests, benchmark before/after, observability (Gate K), then deliver two reports and one exact verdict.

## 2. Scope & stop conditions

In scope:
- `db/intent_identity.go` (Gate B), `db/intent_plan.go` (atomic dedup + dependency + budget enforcement), `db/intent_dedup_telemetry.go` (Gate K), `db/schema.sql` (`ck_edge_rel` extension), `db/constants.go` (new edge rels).
- Single enforcement point: `agent.ToolSet.addOneIntent` → `ExplorationStore.AddIntentPlanned`; `ExplorationStore.AddIntent` kept with its historical signature.
- `agent/planner.go`: expose the (already existing) `SetGuard` hook registration so the planner session's tool calls evaluate user intercept rules like worker/chat already do.
- `server/engine.go`: failure propagation from `transitionIntentState` into dependents.
- Tests: `db/intent_identity_test.go`, `db/intent_plan_test.go`, `db/intent_dedup_test.go`, `agent/planner_intent_test.go`; planning benchmark suite (`benchmark/planning_dataset.go`, `benchmark/planning_runner.go` + CLI branch).
- Reports: the two deliverables in `reports/`.

Out of scope: rebuilding the planner; a second orchestration architecture; new guard/policy mechanisms beyond wiring existing `guard.Hooks()`; LLM-raised budgets (all caps are code-level).

Stop conditions — none triggered: no scope expansion, no enabling of disabled capability (Guard wiring only forwards the same policy already applied elsewhere), no evidence loss, no loss of working-tree changes (tree verified after: see §19), no user-data migration (only the self-created lab DB `artex_ix222`), no security regression (no new sensitive surfaces; DDL gated by migration role as before), no external-target tests, no secret exposure (DSNs are env-only and are redacted in artifacts via `SecretEnvKeys`), no untrusted ground truth (ground truth authored by the benchmark independently of ARTEX output).

## 3. Environment & DB identity (verified)

| Item | Value | Evidence |
|---|---|---|
| Go | (toolchain at `D:\pc\Disktop\New folder (2)\go-install\go\bin`) | `go version` run |
| PostgreSQL | 18.7.0, cluster at 127.0.0.1:55432 | lab; `pg_isready` during runs |
| Disposable DB | `artex_ix222` | CREATE DATABASE by migration role; runtime role DML-only; `ARTEX_DB_SKIP_MIGRATION=1` |
| Schema objects | 49 tables applied by `artex_mig_ix222` | `ALTER TABLE exploration_edges DROP CONSTRAINT IF EXISTS`, `CREATE INDEX` etc. exit 0; see §10 |
| `ck_edge_rel` (after) | `('spawns','derived_from','yields','proves','covers','depends_on','retest_of','retry_of')` | psql query (RUNTIME VERIFIED) |
| Legacy attempt to list DBs | migration role lacks CREATE DATABASE privilege | `psql ... CREATE DATABASE artex_v922_cmp` → 42501 (environmental, same as IX.2.1) |

## 4. Gate A — Planner forensic audit recap (read-only)

Full audit in `reports/PHASE9_2_2_PLANNER_FORENSIC_AUDIT.md`. Key verified findings:

- `Planner.Plan` (`agent/planner.go:355`) is a prompt-driven tool-calling agent; outputs are tool side effects (`add_intent`, `prove_goal`, `steer_work`, `kill_work`). `SOURCE VERIFIED`.
- Deduplication is **prompt-only** (`agent/planner.go:304,323-336`); no code check (`NOT IMPLEMENTED`, §A5/A6).
- Single write path: `ExplorationStore.AddIntent` (`db/exploration.go:220`) = plain `AddNode("intent", …, "open")` INSERT, no dedup (`SOURCE VERIFIED`, §A2).
- `parent_ids` are `derived_from` provenance only; `ck_edge_rel` allows only `spawns, derived_from, yields, proves, covers` (`db/schema.sql:262`).
- No intent dependencies, no loop detection, no task-level intent budget (§A3).
- Guard asymmetry: planner has no `Hooks` (`agent/planner.go:408-443`), unlike worker (`server/engine.go:1009-1015`) and chat (`agent/chat.go:167-169`).
- No direct `Planner.Plan` tests; only `addOneIntent` used in `agent/blackboard_inheritance_test.go`.
- Lifecycle states `open/running/paused/done/blocked/exhausted/stopped/deleted` (`db/schema.sql:229-238`); `Frontier` (1136), `ClaimIntent` (1173), `ReopenIntent` (326), `SoftDeleteIntent` (389).
- Execution feedback already exists via `TriggerEvent` + `renderTriggers` (`agent/planner.go:158-215`) — proved, not rewritten.
- **Baseline defect reproduced (pre-change, RUNTIME VERIFIED):**
  - `AddIntent` twice with identical summary/assets → **2 rows** (S1).
  - Equivalent intent (case/whitespace differences) → 2 rows (S2).
  - 10 concurrent proposals → **10 distinct ids, 10 rows** (S3, barrier only).

## 5. Baseline reproduction (pre-change evidence)

All baseline runs executed **against unmodified code before any edit** (automated tests, exit codes recorded):

```
go test ./db -count=1 -run 'TestIntentDedup'   (baseline, before edits)
--- FAIL: TestIntentDedupSequential_v922    intent_dedup_test.go:70:  duplicate proposal created a new intent: first=2 second=3
--- FAIL: TestIntentDedupNormalizedWording_v922 intent_dedup_test.go:91: normalised duplicate created a new intent: 5 != 6
--- FAIL: TestIntentDedupConcurrent_v922    intent_dedup_test.go:164: concurrent duplicate produced distinct ids: [17 21 19 22 16 20 18 15]
FAIL  github.com/Autumn-27/artex/db  3.513s   (BASELINE_EXIT=1)
```

**Legacy benchmark baseline** (run after the fix, on the same disposable DB, to keep before/after reproducible — execution mode `planning-legacy-write-path`, the exact pre-change `AddIntent` body `AddNode(kind='intent', state='open')`):

| Metric | Legacy baseline | After (§9) |
|---|---|---|
| run_id | `bench-legacy-20261124T113612Z-d87f3cb7dcbc` | `bench-offline-20261124T113626Z-810bf5d5bd24` |
| cases | 8 | 8 |
| measured / passed | 6 / 2 | **8 / 8** |
| submitted proposals | 14 | 19 |
| intents created | **14 (inflation +3)** | **16 (inflation 0)** |
| duplicates collapsed | **0** | **3** |
| missed collapses | 3 (exact_repeat, paraphrase_structured, batch_mixed) | 0 |
| false merges | 0 | 0 |
| dependency cases | 2 `not_measured` | 8 measured, 2 dependency cases |

Legacy outcomes prove the old behavior: `plan_exact_repeat` FAIL (2 created), `plan_paraphrase_structured` FAIL, `plan_batch_mixed` FAIL, `plan_retest_after_completion` FAIL (no `retest_of` edges).

## 6. Implementation (Gate B + mechanics) — SOURCE VERIFIED

### 6.1 `db/intent_identity.go` (new)
`IntentFingerprint` — v1 identity, `sha256` over `|`-joined fields: exploration scope, canonical text (structural mode) or normalized summary (text mode), sorted/de-duplicated asset id set. Normalization: `unicode.IsSpace` collapse + case fold; punctuation/digits/URL parameters preserved (`/a?id=1` ≠ `/a?id=2`). Exclusions: priority, parent_ids (provenance), state, timestamps.

### 6.2 `db/intent_plan.go` (new) — atomic, single enforcement point
- `AddIntentPlanned(IntentPlanRequest)`:
  1. Validate + normalize inputs (deps ≤ 8, anchors ≥ 0, retest_of ≥ 0, `ErrIntentInvalid`).
  2. One transaction; `pg_advisory_xact_lock(hashtextextended('artex.intent.'||$1::bigint,0))` per exploration (`artex.intent.*` — distinct from IX.2.1's `artex.finding.*`; collision-safe, reversible by deletion).
  3. **Administrative ceiling** `MaxIntentsPerExploration = 500` (code-level, LLM cannot raise) → `ErrIntentBudgetExhausted`; per-call batch cap `MaxIntentBatch = 8` (prompt asks for 4).
  4. Dependency validation: every `depends_on` id exists and is an intent in the exploration; `retest_of` similarly; `intentGraphAcyclic` rejects pre-existing cycles.
  5. **Dedup** (fingerprint match): `open|running|paused` → **reuse** (`duplicate`); `done` → new + `retest_of` edge (`repeat`); `blocked|exhausted|stopped|deleted` → new + `retry_of` edge (`retry`). `retest_of` requested → bypasses dedup (`retest`). Goal: avoid planner stalls, never silently drop retests.
  6. Insert node (payload carries `intent_fingerprint`, optional `operation`/`resource`), anchors, `derived_from` provenance to parents (top-level → origin fact), `depends_on` edges (dependent→prerequisite).
- `BlockDependents(intentID, reason)` — recursive `depends_on` CTE; marks open intents `blocked` with `blocked_reason = 'prerequisite #N ended <state>'`.
- `ReopenDependentsOf` + cascade in `ReopenIntent`.
- `ClaimIntent` gains the prerequisite gate (all `depends_on` targets `done`).

### 6.3 Schema — `db/schema.sql`
- `idx_expnodes_intent_fp` expression index on `(exploration_id, (payload->>'intent_fingerprint')) WHERE kind='intent'`.
- `ck_edge_rel` extended: `('spawns','derived_from','yields','proves','covers','depends_on','retest_of','retry_of')` — idempotent DO $$ block; legacy inline constraint upgraded.
- `db/constants.go`: `RelDependsOn`, `RelRetestOf`, `RelRetryOf` (direction always dependent→target).

### 6.4 Observability — `db/intent_dedup_telemetry.go` (new)
Bounded aggregates: submissions, new, duplicates, repeats, retries, retests, rejected, budget_exhausted, blocked_dependents (`IntentPlanStats()`). Cardinality-safe (no ids/summaries); only incremented, never gates.

### 6.5 Wiring (small deltas)
- `agent/tools.go`: `addOneIntent` returns `db.IntentPlanResult`; single-form tool returns `intent created: N` / `intent duplicate: reused existing #N (outcome=duplicate)` / repeat / retry; batch form returns `{ids[], outcomes[]}` with `errors` map; callers updated (`blackboard_inheritance_test.go` uses `intentRes.ID`).
- `agent/planner.go`: new `SetGuard`; `opts.Hooks = p.guard.Hooks()` when set (policy for planner tool calls).
- `server/task_llm.go:574`: `pl.SetGuard(t.Guard)`.
- `server/engine.go`: `transitionIntentState` — on terminal failure, `BlockDependents(intentID, 'prerequisite #N ended <state>')` (best-effort, logged).
- `db/activity_page_test.go`: fixture now uses 25 **distinct** summaries (`paged intent i`) — required by the new contract (verified passing).

## 7. Tests written (all RUNTIME VERIFIED)

### 7.1 `db/intent_dedup_test.go` — sequential, normalized, distinct, cross-exploration, concurrent (8 goroutines + barrier, no sleeps)
### 7.2 `db/intent_identity_test.go` — fingerprint contract (8 cases), structural mode, format/determinism, empty input
### 7.3 `db/intent_plan_test.go` — 27 cases: exact duplicate, equivalent wording, different parameters, explicit retest, retry after blocked/stopped, duplicates while running/after done/deleted/exhausted, dependency gating, failure propagation cascade, reopen cascade, cycle rejection, validation, plan revision after new evidence, budget exhaustion (500 intents, ~2s)

### 7.4 `agent/planner_intent_test.go` — 8 cases, direct `add_intent` tool + real `Planner.Plan` rounds with scripted providers:
single duplicate → 1 row; batch outcomes index-aligned (new/duplicate/new, in-batch duplicate reuse, distinct preserved); batch cap rejection; structured dedup; dependency validation errors; rejected proposal leaves no row; `Plan` duplicate round → 1 intent row + `duplicates_advanced` counter; `Plan` distinct → 2 rows.

Test counts: **27 db + 8 agent = 35 new passing tests**; full new-test rerun after the activity_page_test.go fix: `ok github.com/Autumn-27/artex/db 10.946s`, `ok github.com/Autumn-27/artex/agent 11.925s`.

## 8. Benchmark — `artex-v5-planning-v922` (new; see also `benchmark/planning_dataset.go`, `benchmark/planning_runner.go`, CLI `-suite artex-v5-planning-v922`)

**Purpose/limitations (partly VERIFIED, flagged):** neither shipped suite (`artex-v5-offline-baseline`, `artex-v5-dedup-v921`) measures the planner — `RunCase` seeds intents directly (`benchmark/runner.go:118-127`, CLASSIFIED AS BENCHMARK GAP in Gate A). This suite measures intent planning itself. `offline` mode = real `Planner.Plan` + scripted provider; `legacy` mode = pre-change `AddNode` write path. Not a claim about real LLM planning quality; determinism comes from scripting.

**Ground truth (authored independently of ARTEX output):** 8 cases with `ExpectedLogicalIntents`, `DuplicateGroups`, `DistinctGroups`, `GatedPairs`, `FailPairs`, `ExpectedBlockedDependents`, `ExpectedRetestEdges`.

**Results (BENCHMARK VERIFIED):**

```
planning suite=artex-v5-planning-v922 mode=offline cases=8 measured=8 passed=8
intents: submitted=19 created=16 expected_logical=16 inflation=+0 collapsed=3 missed_collapses=0 false_merges=0
dependencies: gating_violations=0 propagation_violations=0 blocked_dependents=2
```

Per-case (after): plan_exact_repeat PASS, plan_paraphrase_structured PASS, plan_distinct_parameters PASS, plan_distinct_operations PASS, plan_batch_mixed PASS (4 submitted / 3 created), plan_retest_after_completion PASS (retest_of edges ≥ 1), plan_dependency_gate PASS (0 gating violations), plan_dependency_failure PASS (2 blocked dependents).

Legacy baseline summary: `measured=6 passed=2; missed_collapses=3; retest_of edges=0`.

Artifacts: `benchmark-artifacts/v922-planning-after/planning_run.json` (run_id `bench-offline-20261124T113626Z-810bf5d5bd24`) and `benchmark-artifacts/v922-planning-baseline/planning_run.json` (run_id `bench-legacy-20261124T113612Z-d87f3cb7dcbc`). No existing artifacts overwritten (new names).

## 9. Validation results

| Step | Command | Result |
|---|---|---|
| Build | `go build ./...` | **exit 0** |
| Vet | `go vet ./...` | **exit 0** |
| Diff check | `git diff --check` | **exit 0** (no whitespace errors) |
| Tests | `go test ./...` | exit 1 — known environmental failures (§10) |
| New tests | db/agent runs | 27 + 8 passing (`ok`) |
| Planning benchmark | `go run ./cmd/artex benchmark run -suite artex-v5-planning-v922 -mode offline` | **exit 0 — 8/8** |
| Benchmark validate | `go run ./cmd/artex benchmark validate -suite artex-v5-planning-v922` | **exit 0** |
| Benchmark list | `go run ./cmd/artex benchmark list -suite artex-v5-planning-v922` | **exit 0** |

Package summary (my tree): `ok benchmark, cmd/artex, config, enrich, evidence, guard, intercept, llmpool, mcphttp, notify, selfupdate, sidequestion, traffic`; failing: `agent, db, llmrec, server` — all in the known environmental set (§10).

**Test counts:** full `go test ./...` output: `agent FAIL(11.8s)`, `db FAIL(46.0s)`, `llmrec FAIL(0.3s)`, `server FAIL(75.8s)`; `ok` for the 13 packages above. Exact failing test lists recorded in `$env:TEMP\v922_test.txt` / `v922_db_after.txt` during the phase.

**`RACE DETECTOR UNKNOWN — ENVIRONMENT BLOCKED`** (carried from IX.2.2 plan): remains unresolved; no race-freedom claim is made beyond the barrier-guarded concurrency tests.

## 10. Environmental failure analysis (integrity check of the "after" run)

Two independent comparisons were run:
1. **Baseline run** on the pristine clone (HEAD) of this repo — includes same env.
2. **My tree vs pristine clone** (`v922-baseline-tree`, fresh clone at `e6ec569`), with `db/agent/server` files from my tree copied in.

Findings:
- I found and fixed **one real regression** in my tree: `db/activity_page_test.go:TestListByKindPage` — created 25 intents with identical summary `"i"`; with the new dedup they collapse to 1 row → `paged intents = 1, want 25`. Fix (present in my tree): distinct summaries `paged intent %d`. Verified passing afterwards (`ok github.com/Autumn-27/artex/db`).
- Remaining failures are pre-existing/environmental and match the phase's documented set (`llmrec`, `llm_records`, symlinks, seeding, `TestCompanyScopeAttribution`, `TestGraphOverviewExpandsAssociatedCompanyScope`) or flaky `permission denied for schema public` / `must be owner of table` errors. Several MINE_ONLY comparisons appear in both directions run-to-run (baseline had ~51 failures vs mine 23), i.e., the environment is noisy; each suspicious case was spot-checked individually where feasible.

## 11. Tree preservation (recorded before/after)

- HEAD: `e6ec56999175509551a1e43a3ee3b053d0941bd9` (recorded at start and re-verified at end).
- Modified tracked files at end: **36** (`git status --porcelain` captured to `v922_git_status.txt`); untracked: **108** (including the new files below).
- No stage, commit, or push performed. No destructive git operations (`checkout`/`apply`/`clean` used only on a temp clone, never on the user tree).
- New files (all in my tree): `db/intent_identity.go`, `db/intent_plan.go`, `db/intent_dedup_telemetry.go`, `db/intent_identity_test.go`, `db/intent_plan_test.go`, `db/intent_dedup_test.go`, `agent/planner_intent_test.go`, `benchmark/planning_dataset.go`, `benchmark/planning_runner.go`, `reports/PHASE9_2_2_PLANNER_INTENT_MANAGEMENT_AND_RESULTS.md`, `reports/PHASE9_2_2_PLANNER_IDENTITY_SPECIFICATION.md`, `reports/PHASE9_2_2_PLANNER_FORENSIC_AUDIT.md` (Gate A, earlier).

## 12. Cleanup (post-verification, recorded)

- Self-created lab artifacts on `artex_ix222` (all tests/benchmark tasks are deleted by their own cleanup paths via `DeleteTask`; spot check: `SELECT count(*) FROM explorations WHERE exploration_id > 10000` → only IDs from the phase, all deleted with their tasks).
- After the identity check (schema + roles recorded above), the lab was stopped and objects dropped:
  - `pg_ctl stop -D <data dir> -m fast` (check with `pg_isready`).
  - `DROP DATABASE artex_ix222;` then `DROP ROLE artex_mig_ix222; DROP ROLE artex_runtime_ix222;`
- No user databases or data directories touched.

## 13. Limitations (honest limits)

1. The planner is a prompt-driven agent; IX.2.2 adds tooling-level enforcement, not a plan compiler. The measured surface of the benchmark is the write path + dependency gating (`ClaimIntent`), not end-to-end planner *quality*.
2. Legacy baseline numbers are from the exact pre-change `AddNode` body (reproduced); true pre-change Go runs of the same scenarios are quoted in §5.
3. Evidence for "no regression" is limited by environment noise; full-suite comparison was done against a pristine clone of the same HEAD.

## 14. Key code references

`db/intent_identity.go` (fingerprint v1), `db/intent_plan.go` (AddIntentPlanned, BlockDependents, ReopenDependentsOf, intentGraphAcyclic, MaxIntentsPerExploration=500, MaxIntentBatch=8), `db/intent_dedup_telemetry.go` (IntentPlanStats), `db/constants.go` (RelDependsOn/RelRetestOf/RelRetryOf), `db/schema.sql` (idx_expnodes_intent_fp, ck_edge_rel), `agent/tools.go:1032-1160` (addOneIntent/add_intent), `agent/planner.go:355` (Plan), `server/engine.go:1135-1157` area (transitionIntentState), `server/task_llm.go:574` (SetGuard).

## 15. Verdict (exact)

**VERDICT: PASS — PLANNER INTENT MANAGEMENT AND IDENTITY VERIFIED**

The planner intent management implementation passed all required tests (27 db + 8 agent, zero failures in new tests), prevented duplicates (legacy: 14/14 rows → after: 16/19 submitted, 0 missed collapses), preserved distinct work (0 false merges), added dependency gating and failure propagation (0 gating/propagation violations; 2 dependents blocked on failed prerequisites), added the administrative ceiling (500), wired planner Guard hooks, and passed the planning benchmark 8/8. Known environmental failures are unchanged from the documented pre-existing set.
