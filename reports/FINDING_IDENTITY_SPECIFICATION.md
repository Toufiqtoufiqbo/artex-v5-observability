# ARTEX v5 — FINDING IDENTITY SPECIFICATION (IX.2.1)

**Version:** `v1` · **Status:** established before database uniqueness (Gate B7) · **HEAD:** `e6ec56999175509551a1e43a3ee3b053d0941bd9`

This document is the authoritative definition of when two findings are the same logical finding. It is derived from the *actual* data model (`db.RecordFindingInput`, `db.DBFinding`, `db/schema.sql:1081-1109`), not from an imagined model.

---

## 1. Canonical identity fields

| # | Dimension | Source field | In identity? | Rationale |
|---|---|---|---|---|
| 1 | Scope | `findings.task_id` / `RecordFindingInput.TaskID` | **Yes** | Independent assessments must never merge (Gate B3). |
| 2 | Finding type | `vulnclass` | **Yes** | Primary semantic class. |
| 3 | Finding title | `name` | **Yes** | The only stable field available to distinguish resource, parameter and method. Producers **must** encode those distinctions here. |
| 4 | Affected assets | `asset_ids` (JSONB) | **Yes** | Structural target identity already normalized by the asset subsystem. |

**Formula:**

```
fingerprint = "v1:" + hex(sha256(
    "v1"        NUL scope        NUL
    norm_class  NUL norm_title   NUL norm_assets
))
```

`scope` = decimal `task_id` when > 0, otherwise the literal `legacy`.

Implementation: `db/finding_identity.go` → `FindingFingerprint(taskID, vulnClass, name, assetIDs)`.

## 2. Scope and uniqueness policy

- **Per assessment task** for all new findings (`task_id > 0`).
- **`task_id IS NULL`** rows are *legacy/historical* (created when a task is deleted: `ON DELETE SET NULL`). They are **excluded** from the unique index and are never merged, deleted, or rewritten. PostgreSQL's partial index therefore leaves historical findings untouched.
- No global or tenant dimension exists in the current schema; inventing one would be unsafe. Cross-task equivalence is explicitly **not** deduplicated.

## 3. Normalization rules (Gate B4)

| Value | Rule | Rationale |
|---|---|---|
| `vulnclass`, `name` | Unicode lowercase (`strings.ToLower`), trim outer space, collapse any internal whitespace run to one ASCII space | Case/whitespace are presentation noise; all other characters are preserved. |
| `asset_ids` | Drop IDs ≤ 0, remove duplicates, sort numerically, decimal, comma-joined | Set semantics; order and duplicate submissions are not meaningful. |
| Hostname case, default ports, IPv6 form, URL path/query, encoding | **Not normalized here** — delegated to `db/assets.go` (unique asset indexes on domain/ip/url+method; `inet` typing). The fingerprint references stable asset **IDs**, so two representations of the same asset already resolve to the same ID. | Reimplementing URL/IPv6 canonicalization here would risk merging distinct resources. |
| HTTP method / parameter / endpoint | Encoded in `name` by the producer (e.g. `POST Blind SQL Injection (sort)`, `Reflected XSS (?q=)`) | No dedicated columns exist in `findings`; inventing them would be a schema redesign beyond this phase. |

**Documented limitation:** if two *different* issues on the same asset are reported with the *identical* normalized `vulnclass` and `name`, they will be treated as one finding. Producers must make `name` specific. This is a contract requirement, not an implementation defect, and is covered by the "different affected parameter / different HTTP method" tests.

## 4. Excluded mutable fields (Gate B1/B5)

`severity` (mutable via `SetFindingSeverity`), `summary`, `evidence`/`proof`, `worker`, `status`, `report`, `created_at`, `node_id`, `intent_id`, `evidence_version`, `report_evidence_version`, all traffic refs, all notification state.

Reason: a naive whole-object hash would change whenever a report is edited, evidence is added, or triage changes status — splitting one logical finding into several. The benchmark's old `fingerprint(vulnClass, name, severity)` is explicitly **not** reused because severity is mutable and the task/target dimensions are missing.

## 5. Identity versioning

- Every fingerprint carries the `v1:` prefix. A future canonicalization change emits `v2:…`; old rows keep `v1:…` and cannot collide with new ones.
- The database unique index is named `uq_findings_identity_v1` and is **partial**: `WHERE task_id IS NOT NULL AND finding_fingerprint IS NOT NULL`.
- Backfill of historical rows is intentionally **not performed**: it would require a full-table rewrite and could merge historical findings. New rows only.

## 6. Distinct-finding rules

| Situation | Same identity? | Outcome |
|---|---|---|
| Same issue observed repeatedly | Yes | Reuse existing finding; attach additional evidence. |
| Same issue through different evidence | Yes | Evidence attached; original evidence never overwritten. |
| Two separate issues on same endpoint | No (title or asset must differ) | Two findings. |
| Same vuln class, different parameters | No (title differs) | Two findings. |
| Same resource under different methods | No (title differs) | Two findings. |
| Finding materially changed after retest | No automatic identity change | Retest verdict links to original finding; a genuinely new finding gets a new title. |
| Same identity in a different task | No | Two findings (scope differs). |
| Retry after uncertain response | Yes | Idempotent: existing finding returned, evidence binding deduped by snapshot ID. |

## 7. Evidence-association rules

- Evidence is **never** part of identity. A duplicate submission attaches `TrafficRef`s to the existing finding through the existing `AddFindingTrafficTx`, which dedups by `finding_traffic_bindings UNIQUE(finding_id, snapshot_id)`.
- `LockFindingEvidenceTx` + read-time `ValidateFindingEvidence` remain in force. A duplicate cannot acquire unrelated evidence, cross-task evidence stays rejected, and invalidated findings are never revived by dedup.
- Identity resolution happens **inside** `WithEvidenceTx`, under `LockTaskEvidenceTx`, so resolution and evidence attachment are atomic.

## 8. Idempotency semantics

- **Semantic dedup** and **request idempotency** are provided by the *same* mechanism here: the canonical fingerprint within task scope. No separate distributed idempotency service or key store is introduced.
- Request retry after a lost response re-derives the same fingerprint, finds the committed row, and returns it. Side effects (notification event, exploration node) occur only on first insert.
- A request idempotency key is deliberately **not** added: the fingerprint is already stable, task-scoped, and persisted; adding another key would create two conflicting notions of "same".

## 9. Compliance tests

`db/finding_identity_test.go::TestFindingFingerprintContract_v921` is table-driven and covers: identical findings, different types, different targets, different endpoints/assets, different parameters, different methods, missing/empty fields, case normalization, asset order/duplicates, legacy scope, percent-encoding differences, evidence-independent identity, and determinism/format. Result: **PASS** (`go test ./db -run 'TestFindingFingerprint|TestNormalizeIdentityAssets' -count=1`, exit 0).
