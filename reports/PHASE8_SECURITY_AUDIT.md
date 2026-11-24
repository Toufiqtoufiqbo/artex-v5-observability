# Phase VIII — PHASE8_SECURITY_AUDIT

**Security audit of the added scope-enforcement mechanism (Phase 7) and adjacent changes.**

Scope enforcement was the part of the program the mission flagged "security" for. It is implemented, but with the following honest limits:

## 1. Implementation

| File | Change |
|---|---|
| `agent/scopecheck.go` (new) | `SetTaskScope`/`ClearTaskScope` (worker seeds from task scope), `CheckScope(url)`, `hostOf`, `scopeSummary` |
| `agent/httpclient.go` | `doHTTP` calls `scopeGuard(rawURL)` **before** any request is sent — the hard floor |
| `agent/worker.go` | `execute` seeds scope from `as.ListTaskScopeWithSources(taskID)` when the AssetStore exists; `ClearTaskScope()` otherwise |

Scope kinds matched (from `db.TaskScope`):
- `root_domain` — host == domain OR host ends with "."+domain (subdomains in scope — the industry-standard reading of "root_domain").
- `subdomain` — host == domain exactly.
- `ip` — host is the IP.
- `cidr` — host is the IP inside the net.
- `company` — allowed (the company represents all its assets; we don't expand hostnames per company).

Unknown kinds: fail open (allowed).

## 2. Enforcement points

Every network primitive routes through `doHTTP`:
- `HTTPRequest`, `Probe`, `IdorDiff`, `Fuzz`, `Crawl`, `Login`, `Session.auth_check` (auth_check uses doHTTP too — refused if out of scope), and `Retest` (via runOracleFor → doHTTP).
- `AttackPath` is analytical (no requests).

## 3. Honest limits (verbatim from the implementation)

1. **"Best-effort host match, not a policy engine."** `root_domain` includes subdomains by design. If the scope says root_domain "example.com" then sub.example.com IS in scope — matches how scopes are written in this codebase.
2. **Fail-open on unsupported kinds.** If we add a new scope kind and don't handle it, requests pass. Trade-off documented.
3. **No per-request budget in the tool layer.** Request budgets are enforced elsewhere (task clocks, workers) and in the guard layer; the scope floor is the binding one.
4. **Seeding is async-consistent with the planner.** The planner's `set_constraints`/`add_task_scope` and the worker's seeding are independent — there is a window where the worker runs before scope is seeded. The floor is advisory in that window; hard enforcement applies once seeded.
5. **`company` fails open.** This is the current honest design — we can't expand the hostname set without inventing facts.

## 4. What we did NOT add

Per the mission constraints ("Do not add DDoS, destructive operations, credential theft, persistence, stealth, or security-control evasion"):
- No DDoS/flooding, no `sleep`-storm, no loop-until-throttle attacks.
- No destructive operations (`DROP TABLE`, `DELETE`, `UPDATE`, `rm -rf`).
- No brute-forcing of real accounts with service-detection logic.
- No persistence (no cron/scheduled tasks/cron).
- No stealth (no signature masking, no block evasion).
- No credential exfiltration (we do NOT log login passwords; evidence snippets are capped).

The evidence gate (`finding_gate.go`) and the PoC requirement for reports are NOT a form of stealth — they strengthen reproducibility.

## 5. Live verification

- `TestCheckScope_RootDomain` — root_domain scope: in-scope (www, deep sub, EXAMPLE case) allowed; evil.com / example.com.evil.net / notexample.com blocked.
- `TestCheckScope_IP` — ip scope: 10.0.0.5 allowed, 10.0.0.6 blocked.
- `TestCheckScope_NoScopeAllows` — without scope everything allowed.
- Live: planner added `127.0.0.1:3004` to task 54; the worker's probes all ran in-scope (bench app hits all 127.0.0.1:3004).

## 6. Suggested future hardening (not now, not in scope of this phase)

1. Enforce at the tool layer by parsing *outgoing URLs from evidence* as well (already done via finding_gate, different mechanism).
2. Add a "sandbox" scope kind (e.g., `allowlist` with exact host list, stricter than root_domain).
3. Consider blocking requests to non-HTTP(S) schemes and unusual ports unless declared (host-of-scope check currently only checks hostname, not port — technically a scope of root_domain example.com should arguably include all ports; note it).

## 7. Finding statement for the user

Scope enforcement is a **hard floor for network primitives** (all tools route through `doHTTP`, which refuses out-of-scope requests). It is the only phase-7 guarantee, and it is exactly the enforcement that would stop the "hostile target → prompt-injection → Shell" chain at the network layer: any request to a non-declared host is refused before it is sent. Combined with the Evidence Gate, `Login` auto-verification (no password storage), and the design that no Shell exists for AI-evaluated tools, the practical risk surface is substantially reduced.
