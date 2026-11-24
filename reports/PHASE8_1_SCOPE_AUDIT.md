# Phase VIII.1 — PHASE8_1_SCOPE_AUDIT

Mechanism: `agent/scopecheck.go` — `CheckScope(url)` vs task scope (root_domain/subdomain/ip/cidr/company); hooked into `doHTTP` — no request sent out of scope; seeded by `worker.execute` via `ListTaskScopeWithSources`.

Unit: `TestCheckScope_RootDomain`, `TestCheckScope_IP`, `TestCheckScope_NoScopeAllows` — pass.

Live: task 54 — planner scoped 127.0.0.1:3004; all worker probes stayed in-scope (bench-hits.log).

Honest limits (`PHASE8_1_FINAL.md` Phase 7): `company` fails open; unknown kinds fail open; redirect/DNS/concurrent requests not separately tested.
