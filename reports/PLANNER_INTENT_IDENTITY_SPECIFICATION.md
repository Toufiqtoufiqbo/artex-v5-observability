# PHASE IX.2.2 — PLANNER INTENT IDENTITY SPECIFICATION

**Generated:** 2026-11-24
**Target phase:** IX.2.2 — Planner Intent Management & Task Decomposition (Gate B)
**Precursor:** `reports/PHASE9_2_2_PLANNER_FORENSIC_AUDIT.md` (Gate A)
**Evidence labels:** `SOURCE VERIFIED`, `CALL-PATH VERIFIED`, `TEST VERIFIED`, `INTEGRATION VERIFIED`, `RUNTIME VERIFIED`, `PARTIALLY VERIFIED`, `UNKNOWN`, `NOT INVESTIGATED`.

The specification defines **deterministic intent identity**: the single answer to "is this the same unit of work as one already in this task?" — computed at the single write path (`ExplorationStore.AddIntent` → `AddIntentPlanned`) so that the planner, `add_intent`, tests and the benchmark all produce the same identity.

---

## 1. Summary

- Identity is a versioned SHA-256 fingerprint (`v1`) over exploration scope + canonical discriminators.
- Scope is the exploration (one task = one exploration). Identical wording in two independent assessments is never merged.
- Identity is structural when the producer supplies `operation`/`resource`, else it is conservative normalized text (case/whitespace only).
- The asset id set is always part of identity (sorted, de-duplicated).
- Priority, parent_ids (provenance), state, timestamps are excluded.
- Text-only mode keeps punctuation/digits/paths/query parameters — `/a?id=1` ≠ `/a?id=2` (`NOT INVESTIGATED` earlier; now designed conservative).
- Do **not** reuse the IX.2.1 finding fingerprint (mirrored, not reused).

## 2. Fields

| Field | In fingerprint | Rationale |
|---|---|---|
| `exploration_id` | yes (string) | Task scope — the only scope that exists today; no cross-task merging |
| `operation` + `resource` (normalized) | yes — **structural mode** when either is non-empty | Explicit producer naming makes intent recognisable under different wordings |
| normalized `summary` | yes — **text mode** when structural fields are absent | Fallback for legacy `add_intent` callers; conservative normalization only |
| sorted, de-duplicated `asset_ids` | yes | "Same operation, different target" stays distinct; reordering/duplication ignored |

Excluded deliberately: `priority` (scheduling hint), `parent_ids` (provenance: one unit derived from two facts), `state`/`owner` (lifecycle), `created_at`/`completed_at` (presentation).

## 3. Text normalization (`normalizeIntentText`) — SOURCE VERIFIED

```
lowercase, trim, collapse runs of whitespace to single ASCII space;
no change to punctuation, digits, percent escapes, URL paths or query parameters
```

Test evidence (`db/intent_identity_test.go`, RUNTIME VERIFIED):
- `"  test   /LOGIN for   sqli "` ≡ `"Test /login for SQLi"` (normalization applied).
- `"Test /login for SQLi!"` ≢ `"Test /login for SQLi"` (punctuation meaningful).
- `/login?id=1` ≠ `/login?id=2` (test `TestIntentFingerprintDifferentParameters` equivalent in `TestIntentFingerprintContract_v922`).

## 4. Structural mode — SOURCE VERIFIED

When `Operation` or `Resource` is non-empty after normalization: identity = `exp<scope> \x00 op <normalized op> \x00 res <normalized res> \x00 assets <ids>`. Summary is **not** part of identity. Different operation or different resource → different identity.

Tests (`TestIntentFingerprintStructuralMode_v922`, RUNTIME VERIFIED):
- `Summary:"completely different wording"` + same op/res → same fingerprint as base.
- `Operation:"xss-test"` vs `"sqli-test"` → different.
- `Resource:"/login?id=2"` vs `"/login?id=1"` → different.

## 5. Format

`v1:<64 lowercase hex chars>` — `IntentIdentityVersion = "v1"`. Determinism: `TestIntentFingerprintFormat_v922` asserts prefix + length and repeat-call stability (RUNTIME VERIFIED).

## 6. Empty inputs

All-empty input is a valid fingerprint (`TestIntentFingerprintEmptyInput_v922`; non-positive asset ids ignored) — backward compatible with legacy callers that only send a summary.

## 7. Integration points — SOURCE VERIFIED

- `db/intent_plan.go: AddIntentPlanned` computes the fingerprint inside the atomic transaction and stores it in `exploration_nodes.payload->>'intent_fingerprint'` for all intent rows.
- `db/intent_plan.go: findIntentBySummary`-equivalent lookup in the benchmark (`benchmark/planning_runner.go: findIntent`) prefers fingerprint, falls back to summary for legacy rows.
- Dedup semantics live in `db/intent_plan.go` — not in the prompt, not in the planner (Gate A §A1).
- Index: `db/schema.sql: idx_expnodes_intent_fp` — partial expression index on `(exploration_id, payload->>'intent_fingerprint') WHERE kind='intent'`.

## 8. Reversibility — PLAN

- Drop index: `DROP INDEX IF EXISTS idx_expnodes_intent_fp;`
- Drop lookup: remove fingerprint from identity → degrades to conservative text-mode matching (safe direction: more merges, still scoped to exploration).
- No DB constraints introduced (mirrors IX.2.1 rule B4: no uniqueness on identity while states are mutable).

## 9. Test coverage (TEST VERIFIED, runs against disposable `artex_ix222`)

| Test | Intent |
|---|---|
| `TestIntentFingerprintContract_v922` | 8 cases (identical, normalized, asset order/duplicates, cross-scope, different assets, different path/param, punctuation) |
| `TestIntentFingerprintStructuralMode_v922` | Structural identity, op/res variation |
| `TestIntentFingerprintFormat_v922` | `v1:` prefix, 64 hex chars, deterministic |
| `TestIntentFingerprintEmptyInput_v922` | Empty input valid, non-positive ids ignored |

## 10. Evidence classification

| Claim | Classification | Evidence |
|---|---|---|
| Fingerprint exists & computes (`IntentFingerprint`) | SOURCE VERIFIED | `db/intent_identity.go:14-41` |
| Fingerprint used in dedup | SOURCE VERIFIED | `db/intent_plan.go:104-111, 188-195` |
| Fingerprint semantics (normalization/scope/assets) | TEST VERIFIED | `db/intent_identity_test.go` (4 tests, passing) |
| Same scope → same fingerprint | TEST VERIFIED | `TestIntentFingerprintContract_v922` |
| Different scope → different fingerprint | TEST VERIFIED | `TestIntentDedupAcrossExplorations_v922` |
| No reuse of IX.2.1 finding fingerprint | SOURCE VERIFIED | Different code path/naming (`intent_fingerprint` vs IX.2.1's finding fingerprint; not referenced) |
| Index created idempotently | INTEGRATION VERIFIED | psql `CREATE INDEX IF NOT EXISTS` exit 0 |
| Legacy fallback (summary-only rows still matchable) | TEST VERIFIED | `TestIntentDedupSequential_v922` etc. |
