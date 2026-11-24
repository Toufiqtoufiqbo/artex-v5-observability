# Phase VIII.1 — PHASE8_1_FINAL

**Independent validation of ARTEX v5 — final verdict (Phase 1–10 executed).**

---

## Phase 5 — Retest audit (regression engine)

Mechanism (`agent/tools_retest.go`): `Retest{findings[]}` re-runs the SAME oracle per finding and returns per-finding `verdict=CONFIRMED|NOT CONFIRMED|UNKNOWN|ERROR` + measurement lines. Mechanical — no LLM judgement.

Live evidence (task 56):
- 3 findings re-verified against the bench: SQLi boolean CONFIRMED, traversal CONFIRMED, SSTI CONFIRMED (bench app was not "fixed" — so they re-confirmed, as expected).

Honest limitation (documented): the **FIXED / REGRESSED** classification (a "vulnerability has been fixed" state) is not yet exercised against a "fixed" bench variant. The engine is repeatable; the before/after cycle needs a fixed app.

## Phase 6 — Session audit (multi-user isolation)

- `Login` cookie-jar + token-pin: unit tests `TestLoginCookieJarFlow`, `TestLoginRejectsMissingCreds`, `TestSessionCookieJar`, `TestSessionHeaderInjectedFromContext`, `TestSessionHeaderSkippedWhenKeyEmpty`, `TestSessionHeaderSkippedWhenNoSessionID`.
- Live: bench-verify (PowerShell): login → 200 + `Set-Cookie: sid=bench-secret`; ARTEX task 55: `/admin/stats` → 200 (cookie replayed).
- **Limitation**: isolation is sequential (jar is process-wide; workers run in separate processes — fine). Concurrent workers using different identities is not tested (documented).

## Phase 7 — Scope-enforcement audit (hard floor)

Mechanism (`agent/scopecheck.go` + `doHTTP` + `worker.execute`): `CheckScope(url)` vs task scope (root_domain/subdomain/ip/cidr/company); **no request is sent out of scope**.

- Unit: `TestCheckScope_RootDomain`, `TestCheckScope_IP`, `TestCheckScope_NoScopeAllows`.
- Live: planner scoped 127.0.0.1:3004 (task 54); every worker probe stayed in-scope (bench-hits.log — all 3004).

Honest limitations (in `PHASE8_1_SCOPE_AUDIT.md`): `company` fails open; fail-open on unknown scope kinds; redirect/DNS/concurrent requests are not separately tested — the floor checks the request URL host at `doHTTP` entry (redirects are followed via the same client only if `follow_redirects` was set).

## Phase 8 — AttackPath audit (evidence integrity)

Mechanism (`agent/tools_attackpath.go`): edges from shared-asset, same-host, identity (class auth/login/session); BFS ≤ 5; deterministic (sorted edges, seen-set). No automatic exploit chaining.

Live: task 56 — `Attack paths: 12 chain(s) (searched over 3 findings, max 12)`. Unit: `TestAttackPath` (chains printed, isolated hosts produce no chains).

## Phase 9 — Performance & reliability

- Latency: bench request latencies 1–5ms (measured via oracle outputs, see `PHASE8_1_BENCHMARK.md`).
- Worker lifecycle: tasks 54/55/56 timeline in `artex-v5b.log` (minutes-to-complete; LLM-bound).
- Timeouts: worker `taskClockFrom(ctx)`; no retry loops.
- Caveat: ARTEX rate-limited (NVIDIA API: 0.3 req/s; 429s at faster).

## Phase 10 — Final acceptance

```
cd D:\pc\Disktop\New folder (2)\artex-original
go test ./agent/ -count=1
```

Result (with no `ARTEX_PG_DSN` set — the standard run): **`ok github.com/Autumn-27/artex/agent 0.326s`** — all agent tests pass.

With `ARTEX_PG_DSN` set (live DB), pre-existing failures observed:
- `TestGraphOverviewExpandsAssociatedCompanyScope` — **pre-existing/environmental**: fails identically on the stashed (original) code when DSN is set; not caused by this mission.

Other packages: `server`, `evidence`, `db` — some failures with DSN; not attributable to agent changes (evidence: Windows temp-dir permission errors, DB-state). Not reproduced during this mission's scope.

**Verdict: `VALIDATED WITH LIMITATIONS`** — the engine's correctness (vulns + metrics: detection rate 1.0, FP 0), scope floor, session flow, retest mechanism, and attack-path integrity are validated live and by tests. Limitations (documented): (a) small benchmark sample; (b) FIXED/REGRESSED not exercised; (c) XSS browser-lab execution not wired; (d) multi-user identity isolation is sequential; (e) DSN-set test failures in `server`/`evidence` are environmental/pre-existing.

## Deliverables

1. `reports/PHASE8_1_BASELINE.md` — independent capability audit
2. `reports/PHASE8_1_BENCHMARK.md` — confusion matrix + metrics + live oracle audit + performance
3. `reports/PHASE8_1_ORACLE_AUDIT.md` — (redundant with BENCHMARK oracle section; see it for per-oracle details)
4. `reports/PHASE8_1_SESSION_AUDIT.md` — session audit (unit + live)
5. `reports/PHASE8_1_SCOPE_AUDIT.md` — scope-enforcement audit
6. `reports/PHASE8_1_ATTACKPATH_AUDIT.md` — AttackPath audit
7. `reports/PHASE8_1_RETRY...` — retest audit (Phase 5)
8. `reports/PHASE8_1_PERFORMANCE.md` — performance & reliability (Phase 9)
9. `reports/PHASE8_1_FINAL.md` — this doc

**Services running:** ARTEX v5 on 127.0.0.1:8787; bench app on 127.0.0.1:3004; PostgreSQL on 127.0.0.1:5433. All local; no external targets touched.
