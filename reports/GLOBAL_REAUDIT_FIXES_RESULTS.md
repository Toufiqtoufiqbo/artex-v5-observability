# GLOBAL RE-AUDIT FIXES — F1 + F2
## Implementation & Verification Report

**Generated:** 2026-11-24
**HEAD:** `e6ec56999175509551a1e43a3ee3b053d0941bd9`
**Lab:** disposable DB `artex_v923_fix` (created/dropped in this task).

---

## 1. F1 — Planner event duplication (FIXED AND RUNTIME VERIFIED)

**Root cause:** `agent/planner.go` emitted the same `*Event` pointer twice without changing `EventType` → two `PLANNER_PLAN_STARTED`, zero completion; plus a second completion in `server/engine.go`; plus a mislabeled `PLANNER_GOAL_DECOMPOSED` emitted from `Plan()`.

**Changes:**
- `agent/planner.go`: completion now sets `planEv.EventType = PlannerPlanCompleted` and calls `.End()` before emitting; removed the `PlannerGoalDecomposed` emission.
- `server/engine.go`: removed duplicate `PlannerPlanCompleted` emission in `runRound` (met branch).
- New test: `agent/planner_telemetry_fix_test.go::TestPlannerPlanEventsAreNotDuplicated_v923`.

**Runtime evidence (DSN=artex_v923_fix):**
```
event type=PLANNER_PLAN_STARTED task=13
event type=PLANNER_PLAN_COMPLETED task=13
event type=PLANNER_INTENT_CREATED task=13
--- PASS: TestPlannerPlanEventsAreNotDuplicated_v923
```

## 2. F2 — IX.2.1.1 finding empty-name fallback (FIXED AND RUNTIME VERIFIED)

**Changes:**
- `db/finding_identity.go`: `FindingNameOrDefault(name, summary)`.
- `agent/tools.go::report_finding`: passes `db.FindingNameOrDefault(a.Name, a.Summary)` into `RecordFindingInput.Name`.
- New test: `db/finding_identity_empty_name_test.go::TestFindingNameOrDefaultEmptyName_v923`.

**Runtime evidence:** all finding-dedup tests + new test passed with DSN (`ok github.com/Autumn-27/artex/db 6.905s`).

## 3. Validation

| Command | Exit |
|---|---|
| `go build ./...` | 0 |
| `go vet ./agent ./db ./server ./telemetry` | 0 |
| F1 test with DB | 0 (PASS) |
| F2 + IX.2.1 finding tests with DB | 0 (PASS) |

## 4. Verdict (exact)

**VERDICT: PASS — GLOBAL RE-AUDIT FINDINGS F1/F2 FIXED AND VERIFIED**
