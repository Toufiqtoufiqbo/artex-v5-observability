# Phase VIII.1 — PHASE8_1_ATTACKPATH_AUDIT

Mechanism: `agent/tools_attackpath.go` — nodes=findings; edges from shared-asset, same-host, identity (auth/login/session class); BFS chains ≤ 5; deterministic (sorted, seen-set). Conservative: no auto-exec, no privilege escalation.

Live: task 56 — `Attack paths: 12 chain(s) (searched over 3 findings, max 12)`. Unit: `TestAttackPath` — chains listed; isolated hosts produce no chains.

Integrity notes: edges derived from measurable facts only (shared asset, host, identity class). See `PHASE8_1_FINAL.md` Phase 8.
