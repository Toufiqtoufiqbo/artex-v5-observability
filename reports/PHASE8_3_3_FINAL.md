# PHASE8_3_3 — FIXTURE PARITY + FINAL

**Fixture:** `bench-vuln.py` :3004, `GET /ok` → 200 ok (verified this phase). Ground truth: `bench-truth.json` / `bench-metrics-truth.json` (6 positives, negatives). Accounts: synthetic (`sid=bench-secret`, alice/bob objects). Reset: restart the fixture process (no persistent state besides append-only `bench-hits.log`). No external destination required.

Parity: both ARTEX instances can reach :3004; baseline :8888 and modified :8787 are isolated and healthy. However, **neither version can run a detection task without an LLM** (see LLM inventory). LLM configuration parity was not established (baseline has `no LLM provider configured`; v5 uses NVIDIA endpoint), so paired LLM runs would be asymmetric.

**Verdict: `PARITY BLOCKED`**

Proven: all four services alive; both health 200; fixture reachable; common deterministic detection subset is empty; baseline LLM not configured → asymmetric conditions.

Not performed: RULE 5 deterministic benchmark (no deterministic detection task exists), RULE 6 LLM benchmark (asymmetric provider config + rate limits), scoring, per-category comparison.

Required to unblock: configure equivalent LLM providers in both versions, then run paired LLM tasks with fixture reset between them; or provide a deterministic CLI oracle entry point in both versions (would require code changes — out of scope here).

No superiority claim made. Reports not produced: `PHASE8_3_3_DETERMINISTIC_RESULTS.md`, `PHASE8_3_3_LLM_RESULTS.md`, `PHASE8_3_3_REGRESSION_REVIEW.md`.
