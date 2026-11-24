# PHASE8_3_5 — REGRESSION REVIEW (source + runtime)

## 0. First-order finding: the candidates are different upstream revisions

This is the single most important limitation of the whole comparison and it must frame every result.

| | Baseline archive | Modified v5 |
|---|---|---|
| Identity | "ARTEX **global edition** (this fork)" — per its own `CHANGELOG.en.md` | upstream Autumn-27 ARTEX at `e6ec569` (detached HEAD) + local additions |
| i18n subsystem | present (`server/errmsg_catalog.go`, `errmsg_i18n.go`, `agent/outputlang.go`, `intercept/label_i18n.go`, ~40 `*_localized_test.go`) | **absent** |
| `server/auth.go` size | 24,777 bytes | 9,678 bytes |
| norma | v0.4.4 | v0.4.3 |
| Commit identity | **unverified** (no `.git`, no metadata in zip) | `e6ec56999175509551a1e43a3ee3b053d0941bd9` |

Consequence: a difference between the two candidates **cannot** be attributed to the v5 additions alone. It may come from the upstream base divergence. All comparative statements in this phase are therefore conditional.

## 1. Capabilities present in baseline, missing in modified v5

| Capability | Evidence | Impact |
|---|---|---|
| `ARTEX_ADMIN_PASSWORD` headless bootstrap | baseline `server/auth.go:36,248-265`; **zero** references in modified tree | **Functional gap.** On a fresh DB, baseline auto-initialises the admin password; modified returns `403 密码未初始化，请先设置密码` and needs `POST /api/auth/init`. Reproduced live in this phase. |
| i18n / localized error catalog | baseline files listed above | UI/API language coverage; not detection-related |
| `detections/` rule corpus (sigma/suricata/indicators) | baseline dir, not imported by Go code | content only, not runtime |
| cloud build/docs scripts | `build-cloud.sh`, `start-cloud.sh`, `scripts/`, changelog, SECURITY docs | packaging/docs only |

None of these are detection-logic regressions **introduced** by v5; they are capabilities of a newer/different upstream base that the frozen v5 tree does not contain. They are still recorded because the phase asks for "functionality present in baseline but missing in v5".

## 2. Capabilities added in modified v5 (not a regression)

New files under `agent/` (all absent from baseline):

```
agent/scopecheck.go        agent/httpclient.go          agent/finding_gate.go
agent/tools_probe.go       agent/tools_http.go          agent/tools_login.go
agent/tools_diff.go        agent/tools_attackpath.go    agent/tools_retest.go
agent/strategy.go
(+ 6 test files)
```

- **Transport-level scope guard**: `doHTTP` is the single transport; `CheckScope` runs before `client.Do`; company-type scope entries fail closed; redirects are revalidated per hop (10-hop bound, credentials stripped cross-origin); `dialFn` hook allows dial-count proof.
- Baseline has **no** equivalent transport-level scope enforcement (`scopecheck.go` absent; enforcement is prompt-level only). This is a security *addition*, not a parity item.

## 3. Shared-capability regression check

| Shared capability | Baseline | Modified | Regression? |
|---|---|---|---|
| Task create / planner / worker loop | works | works (pilot) | no |
| LLM provider resolution | `task_llm.go` identical | identical | no |
| Health endpoint | 200 | 200 | no |
| Login | 200 (env bootstrap) | 200 (after `/api/auth/init`) | **gap** (see §1) |
| Findings persistence | works | works (pilot) | no |
| Agent tests | `go test ./agent/` ok | `go test ./agent/` ok (VIII.3.1) | no |

## 4. Security-control weakening check

No detection logic, prompt, or tool schema was modified in this phase. The only source-level change made anywhere was compiling the baseline (`go build`), which does not alter its tree. v5's scope guard, redirect revalidation, and dial hook remain intact and were exercised by the pilot (all fixture traffic stayed on `127.0.0.1:3004`).

## 5. Verdict of this review

- **Regression introduced by the v5 additions:** none observed in shared capabilities.
- **Capability gap due to base divergence:** `ARTEX_ADMIN_PASSWORD` headless bootstrap (confirmed live), plus i18n and packaging content.
- **Security posture:** v5 is strictly stronger at the transport layer (scope enforcement exists only in v5), but this is a *new* capability and is excluded from the detection score.
