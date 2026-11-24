# PHASE8_4_3 — REGRESSION REVIEW (Phase J)

## 1. Diff summary

```
Changed files by this phase (backup diff):
  CHANGED  agent/scopecheck.go            (no change — VIII.4.1/2 preserved; byte-identical, see hashes)
  CHANGED  agent/httpclient.go            (audit events + egressHTTPProxyPolicy)
  NEW      agent/egress_policy.go
  NEW      agent/egress_policy_test.go
  CHANGED  traffic/egress.go              (new)
  CHANGED  traffic/traffic.go             (upstream hook + audit emission)
  NEW      traffic/egress_test.go

NOT modified by this phase (VIII.4.1/2 preserved):
  agent/scopecheck.go — VIII.4.1/2 implementation intact (verified hashes match VIII.4.2 POST hashes)
  agent/worker.go — VIII.4.2 changes intact (per-task store, guard, containment options)
  agent/bash_guard.go, agent/scope_task_isolation_test.go — intact
  agent/tools*.go — unchanged since VIII.4.2

Pre hashes:  backups\phase8_4_3\hashes.txt (PRE)
Post hashes: appended (POST)
```

## 2. VIII.4.1/2 preserved checks

| Control | VIII.4.1 status (re-run) | VIII.4.2 status (re-run) |
|---|---|---|
| Unseeded/empty/malformed → deny (with zero dials) | PASS | PASS |
| Out-of-scope → deny | PASS | PASS |
| Valid scope → allow | PASS | PASS |
| Redirect revalidation + credential stripping | PASS | PASS |
| Dial accounting | PASS | PASS |
| Per-task isolation (unit) | — | PASS |
| Worker entry enforcement | — | PASS |
| Bash guard | — | PASS |
| WebFetch/WebSearch disabled | — | (code-verified) |
| MCP dropped from worker | — | PASS |

## 3. Controls reviewed after changes

| Control | Status |
|---|---|
| Redirect validation active | PASS (`TestRedirectNoDial`) |
| Cross-origin credential stripping | PASS (unchanged) |
| Dial-level accounting | PASS (unchanged, `withDialCounter`) |
| Valid scope semantics preserved | PASS |
| Provider config untouched | code-verified; norma module not modified |
| Detection logic untouched | grep — no changes in tools_*.go |
| Prompts/schemas untouched | code-verified |
| Uncommitted changes preserved | verified (git status diff + hashes) |
| Secrets in source/reports | strict scan: none |

## 4. Behavioral changes (intended)

- `doHTTP` no longer uses `http.ProxyFromEnvironment` — target-assessment clients go direct (VIII.4.3 Phase B).
- `doHTTP` now emits audit events.
- Traffic proxy: allowlist (empty default = permissive; `SetEgressAllowlist`); audit events.

## 5. New capabilities (by design)

- Traffic proxy explicit destination allowlist (empty = legacy behavior).
- Structured egress audit events (3 categories; observational).
- Containment: WebFetch/WebSearch off, MCP dropped from worker.

## 6. Not-regressions (documented limits)

1. Enrichment egress — documented limitation (VIII.4.3).
2. Self-update — documented follow-up.
3. Bash heuristic — documented limits.
4. Interactive shell tools — documented follow-up.
5. IPv6 `hostOf` — unchanged limitation.
6. Live egress allowlist API for the traffic proxy — not exposed at runtime (follow-up).

## 7. Regression verdict (Phase J)

`PASS` — VIII.4.1/2 guarantees intact; new controls contained.
