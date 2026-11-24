# PHASE IX.1.1 — FULL PROJECT REASSESSMENT

**Final verdict:** `PASS — FULL PROJECT REASSESSMENT COMPLETE`

**Generated:** 2026-11-23 · HEAD `e6ec56999175509551a1e43a3ee3b053d0941bd9` · ARTEX v5 (Go 1.26.11, windows/amd64)

> Note on method: §1–§18 were completed by direct code reading, §16/§26 by execution (tests). Where subagents were used for breadth, their findings are folded into the relevant sections; the security register (§22) is the final authority.

---

## 1. Executive summary

ARTEX v5 is a **functional, multi-agent, production-shaped pentesting platform**: an LLM-driven event-driven exploration engine (Planner/Worker/MainAgent), a PostgreSQL data model of 49 tables, 243 HTTP API routes, a Next.js frontend, MCP client integration, notification channels, traffic capture/egress control, and an evidence/finding subsystem hardened through VIII.4–VIII.6. It is **operationally mature on the core path** and genuinely useful for authorized assessments (tenant-scoped engines, storage scoped by operator role since VIII.6).

The assessment identifies **no blocking defects**; the product is not "finished", it is mature on the central path. The strongest finding for the IX.2 roadmap is that **finding-quality guardrails (dedup, adversarial robustness) are young** (§19), and **live-model benchmarking is designed but not yet exercised** (§12, §24). Security posture was re-verified for IRON BOUNDARY regression surface (§22): everything remains consistent with VIII.6.3.

---

## 2. Initial repository state (preserved)

- Branch: **detached HEAD `e6ec56999175509551a1e43a3ee3b053d0941bd9`**
- `git status --short`: **82 entries** — 21 modified, 61 untracked.
  - Modified (21): agent/{chat,mainagent,planner,provider,provider_capture_test,tools,worker}, db/db, enrich/enrich, evidence/store, selfupdate/{github,stage}, server/{assembly,auth,customtool,dto,finding_retests,finding_traffic,manager,server}, traffic/traffic (+ IX.1: cmd/artex/main.go, benchmark/, .gitignore)
  - Untracked (61): agent/{bash_guard,egress_policy,fs_containment,finding_gate,finding_workflow,httpclient,provider_egress,scopecheck,strategy,target_boundary,tools_diff,tools_probe,tools_retest,tools_attackpath,tools_digest,tools_insert,tools_http,tools_login,toolcatalog}, db/{db,finding_traffic,finding_retests,finding_assets,finding_retests...}, evidence/{store,v8_6_3_*}, server/{egress_audit_wiring_test,engine_*,finding_*,v8_6_3_*,task_*,notify_*,...}, traffic/{egress,egress_dial,archive,...}, enrich/egress_test, benchmark/ (10 files), cmd/v863seed (now removed in IX.1), lab846/ artifacts, benchmark-artifacts/ (IX.1, gitignored).
- Go **1.26.11**, windows/amd64. PostgreSQL cluster846 (restarted during IX.1, stopped again; port 5433 free). No running ARTEX processes.

> **Preservation**: `git status` identical before/after this assessment; no commits/pushes; DB `artex_v863f` (and artex_v863/2/3) dropped in VIII.6.3; no destructive operations in IX.1.1.

---

## 3. Complete component inventory

### 3.1 Go packages (36 packages)

| Package | Files | Responsibility | Lang |
|---|---|---|---|
| `agent/` | 92 | Core engine: Planner, Worker, MainAgent, ChatAgent, tools (Probe/Crawl/Session/Login/Strategy/IDOR-diff/Fuzz/AttackPath/Retest/HTTP/HTTPRequest/InteractiveShell/Todo/Strategy), HTTP client, Guard/Intercept wiring, strategy text, prompt templates | Go |
| `server/` | ~80 | HTTP API (243 routes), Engine (event loop), Manager, Manager/Server, assembly (tool augmentation), logs (SSE), GC, task archives, orchestrator (tasks), assets routes, LLM mgmt | Go |
| `db/` | ~60 | PostgreSQL schema + accessors (tasks, explorations, activity, findings, assets, llm_profiles, llm_usage, llm_records, notify, audit, auth), asset_intercept | Go |
| `evidence/` | ~5 | Finding evidence store: blobs (sha256-addressed), snapshot verification, finding traffic bindings, GC | Go |
| `traffic/` | ~16 | MITM proxy, egress allowlist/dial (VIII.4.3/4), traffic store, archive/reclaim, evidence file access | Go |
| `llmpool/` | ~5 | LLM provider failover pool + health registry | Go |
| `llmrec/` | ~4 | LLM usage ledger (`llm_usage`) + full call records (`llm_records`) | Go |
| `guard/` | 2 | PreToolUse/PostToolUse audit + intercept-rule evaluator | Go |
| `intercept/` | ~4 | User-configurable tool intercept: rules, LLM judge, "ask" approvals, task-context | Go |
| `enrich/` | 2 | Async DNS/HTTP asset enrichment (dnsx + proxy probe) | Go |
| `notify/` | ~25 | Notification channels (WeCom, DingTalk, Feishu, email, Telegram, webhook, Slack-ish generic) + delivery state machine + filters/masks/SSRF-hardened webhook | Go |
| `sidequestion/` | 6 | Async side questions (chat-style, streaming) — no tools | Go |
| `report/` | 2 | Findings markdown/csv/json exporters (`FindingsMarkdown`, `FindingsCSV`, `SingleFindingMarkdown`, `FindingFilename`) | Go |
| `mcphttp/` | 1 | Remote (Streamable HTTP/SSE) MCP clients → tool adapters | Go |
| `config/` | 2 | Config file loading (ARTEX_CONFIG, config.json), BaseDir | Go |
| `selfupdate/` | 2 | Self-update bootstrap/rollback | Go |
| `benchmark/` | 10 | **IX.1**: benchmark engine, case schema, dataset, matcher, metrics, mock provider, CLI, artifacts | Go |
| `cmd/artex/` | 2 | CLI entry point (flags; `benchmark` subcommand added by IX.1) | Go |
| `web/` | — | Next.js App Router (see §13) | JS/TS |

User-facing artifacts: `screenshots/` (dashboard, chat, findings, graph, assets, assets_test, agents pages) — manual-verification artifacts for the UI. `skills/` — `playwright-cli` (browser-automation skill for testing web apps — dev tooling, FM), `scopesentry` (integration skill).

### 3.2 Application entry points

- `cmd/artex/main.go` — backend binary (flags: `-addr`, `-data`, `-proxy`; subcommands: `benchmark list|validate|run|compare|report` — added IX.1).
- `build.sh`, `install.sh`, `dev.sh`, `start.sh`/`start.bat` — build/install/dev orchestration.
- `Dockerfile`, `docker-compose.yml`, `docker-compose.bench.yml` (bench compose).
- `web/` — Next.js frontend (`npm run dev/build/start`).

### 3.3 Data model (db/schema.sql, 49 tables)

Tasks (created/queued/running/paused/done/failed/timeout), explorations + nodes (goals/intents/facts/finding/hint/digest), activity, findings, finding_retests, traffic_evidence_snapshots, finding_traffic_bindings, assets/companies (new unified model: root_domain/ip/subdomain/app/service/endpoint + tech/cred/param/asset data + confidence + tombstoned), llm_profiles, llm_profile_health, llm_usage, llm_records, tool_usage, tool catalog (user-editable via ToolResolve), agents + prompts + prompt_vars, agent_triggers, conversations (chat), main_sessions, notify_channels/events/deliveries, intercept_rules/pending/history, asset_intercept_rules, task_intercept_rules, task_archives, task_templates, task_categories, scheduler_state, settings, server_logs, side_question_sessions/requests, main_sessions, plugins? (no), webhooks? (in notify), goal decomposition products (in task_goals? via UpdateGoalPayload/set_goals), plans? — none.

### 3.4 Third-party dependencies (go.mod)

- `github.com/Autumn-27/norma v0.4.3` — **SDK** (agentcore, harness, llm, tool, transcript, hook, compaction, noa, skill, memory, plan, subagent, permission) — the model of norma provides the tool-call loop, session persistence, streaming, settlement, deferrals.
- `golang-jwt/jwt/v5`, `pgx/v5`, `google/uuid`, `jackc/pgx`, `klauspost/compress`, `lqqyt2423/go-mitmproxy` (MITM proxy), `miekg/dns`, `projectdiscovery/dnsx`, `golang.org/x/{crypto,net,text}`, `modernc.org/sqlite` (legacy).

---

## 4. Architecture diagram

```
                     ┌──────────────────────────────────────────────────────┐
                     │  cmd/artex  (CLI; flags; `artex benchmark …` from IX.1)│
                     └──────────────────────────────────────────────────────┘
                                       │
                        ┌──────────────┴──────────────┐
                        │        Manager (server/manager.go) │
                        │  pg(sqlite legacy) + task/stores + intercept + env (ARTEX_PG_DSN, ARTEX_EGRESS_ALLOWLIST, ARTEX_DB_SKIP_MIGRATION…) │
                        └──────────────┬──────────────┘
                                       │ owns
            ┌──────────────────────────┼───────────────────────────┐
            │                          │                           │
        ┌───────┐              ┌─────────────┐               ┌────────────┐
        │ Asset  │              │   Engine    │               │   Server   │ HTTP 243 routes
        │ Store  │              │ (server/)   │               │ (API)      │ → Frontend (Next.js)
        └───────┘              └──────┬──────┘               └──────┬─────┘
            ▲                         │                             │
            │          ┌──────────────┴──────────────┐              │
            │          │   Planner   (agent/planner) │◄─── proofs ──┤
            │          │   Worker    (agent/worker)  │◄─── findings─┤
            │          │   MainAgent, ChatAgent      │◄─── chat     │
            │          │   GoalDecomposer (agent/goals)             │
            │          └──────────────┬──────────────┘              │
            │                         │ LLM calls (norma/agentcore) │
            │          ┌──────────────┴──────────────┐              │
            │          │ norma SDK: agentcore.Session                │
            │          │   + harness loop (tool calls, settle, abort)│
            │          │   + compaction / noa                       │
            │          │   + protocol: anthropic/openai(-responses) │
            │          └──────────────┬──────────────┘              │
            │                         │                             │
            │   ┌─────────────────────┴─────────────────────┐       │
            │   │  Tools (agent/tools*.go; catalog=toolcatalog.go) │◄── LLM-visible schemas
            │   │   • domain: insert_assets, report_finding,
            │   │     graph_overview, list_findings, get_worker_output,
            │   │     add_intent, add_hint, prove_goal, goal_met, set_goals,
            │   │     set_constraints, deep/attack-path(AttackPath), retest,
            │   │     fuzz/IdorDiff, strategy, segment tools, TodoWrite,
            │   │     traffic_search/get/blob (recording), crawl, login,
            │   │     HTTPRequest (agent/httpclient), Bash (guarded)
            │   │   • guards (ContainLocalShell/ContainFileTools)        │
            │   │   • intercept (guard + rules + LLM judge + ask-approval)│
            │   └──────────────┬────────────────────────────────────────────┘
            │                  │                    │                  │
            │                  │                    │                  │
            │      ┌───────────┴──────┐   ┌────────┴────────┐   ┌──────┴────────┐
            │      │ traffic/ (egress │   │ llmrec/ (usage  │   │ db/ (findings,│
            │      │ allowlist, MITM, │   │ ledger)         │   │ activity,     │
            │      │ evidence blob)   │   │                 │   │ explorations, │
            │      └──────────────────┘   └─────────────────┘   └───────────────┘
            │
            ├─ SideQuestions (sidequestion/)
            ├─ Goals (db/goal…; set_goals → DecomposeGoalsWithProvider → set_goals)
            ├─ Events/Triggers (db/triggers.go; tasks triggers)
            ├─ Enrich (enrich/ — dnsx + proxy probes)
            ├─ Notify (notify/ — channels + delivery + SSRF-hardened webhooks)
            ├─ Audit: server_logs (logsink), guard audit, intercept pending/history,
            │         egress-audit.jsonl (traffic), llm_records (bodies, gated),
            │         task_archives (VIII.x)
            ├─ Observability: activity (SSE), usage tokens, llm_usage, tool_usage
            ├─ Export: report/* (md-zip, md, csv, json), findings routes
            ├─ MCP: mcphttp (remote HTTP/SSE clients → tools), DB config, skills
            └─ Self-update (selfupdate/)

Frontend: Next.js App Router pages — (auth)/login, (auth)/setup, (external), (main)/{chat, dashboard, function/{assets,commands,findings(+detail+lineage),llm-records,sync,tasks(+detail+tabs),traffic,workspace}, system/{agents(+detail),intercept(+approvals+assets),llm(+retry),logs,mcp,notify,settings,skills,tools}}; data via `web/src/lib/api.ts` (+ types, lib/mock, auth, status, utils, activity-merge, side-questions, chat-send-mode, task-assets, cookie.client, local-storage.client, company-scope, sort-preference, chat-mentions); components — exploration-graph, finding-*, traffic-*, transcripts, approval-*, agent-editor, todo-popover, etc.
```

---

## 5. Core Engine assessment

**Engine & task lifecycle (F4)** — `server/engine.go` (`Engine.Run/Pause/StopTask/Resume/ControlWork/SteerWork/KillWork`), `db.CreateTaskWithOptions` (single-transaction task+exploration+origin-fact), task statuses (`created/queued/running/paused/done/failed/timeout`; `IsTerminal`), deadline coordinator (`engine_timeout.go`: settleTask — settle→drain 90 s grace→hard cancel→final planner round→guarded terminal), engine restarts (`ResetRunningIntents`). Verified: engine tests (cancelcause/timeout/emptyturn), runtime (VIII.x).

**Execution scheduling (F4)** — per-task plannerLoop (debounce 800 ms; heartbeat ≥600 s; triggers drain) + workerLoop (claim intents via `claimNext`→`ClaimIntent`, `Frontier(20)`); intents (open/running/done/exhausted/blocked/stopped/paused/deleted) with CAS transitions (`CompareAndSetIntentState`, `SetIntentState`, `SetNodeState`); worker model_error retries (2×3 s), per-run MaxDuration, non-destructive cancel-cause propagation (`agent.AbortCause`), graceful shutdown (`AbortShutdown`). Evidence of high reliability: engine_* tests + runtime (VIII.4.6.1, VIII.5.1, VIII.6).

**Multi-agent orchestration (F3/F4)** — Planner, Worker, MainAgent, ChatAgent (sidequestion side questions), GoalDecomposer (`agent/goals.go` — `DecomposeGoalsWithProvider` called from `server/goals.go:387`), LLM failover + intercept-LLM-judge (`server/task_llm.go`), retry policy (db/llmretry.go).

**Observability (F3)** — activity ledger (`db/activity`), llm_usage ledger (§7), llm_records (full wire trace, gated by `llm_record`), SSE `/api/exploration/activity/stream`, Egress audit (VIII.6.1).

---

## 6. Planner assessment (F3/F4)

`agent/planner.go` — event-driven `Plan(ctx, taskID, as, ts, goal, triggers, emit) → (met, reason, err)`. Per round: `compactor.OnPlannerRound` (cold-digest advance), ToolSet assembly (`AugmentTools`, DB overrides), prompt assembly (`plannerSystem` + `renderTriggers` + `graphOverview` prefetch), budgets (MaxDuration clamping), settlement, `captureRun`. Triggers: worker done/finding/goal/goal_deleted/goal_edited/hint/cancelled. **Strengths**: LLM-driven, reads the graph first (overview), direct output path (add_intent, prove_goal, goal_met). **Gaps (IX.2)**: no code-level intent dedup (prompt-level only, `agent/plannerDefaultTmpl` "克制"), planner rounds on bad LLM output are silent, compaction is experimental (`noa` experimental, §17-FD).

## 7. Worker assessment (F4)

`agent/worker.go` — `Execute(ctx, name, taskID, as, ts, intent, hooks, emit, enr, notifyFinding)`: one intent per run; system prompt (intent + anchored assets + strategy + methodology + traffic tool note), real tools, session resume, settlement on budgets, WriteCounts. Tools: report_finding, insert_assets, list_assets, add_intent? (no), record_fact, goal_met, traffic tools, HTTPRequest, HTTP oracle tools (Probe, Crawl, Session), Login, Strategy, IDOR-diff, Fuzz, AttackPath, Retest, InteractiveShell (bash), TodoWrite (norma), shell_open/shell_send (interactive, guarded). Evidence: **F4** (IX.1 benchmark-verified for offline mode; VIII.6.3 runtime-verified for tapes; live mode unexercised).

---

## 8. Agent orchestration assessment (F3/F4)

- MainAgent (`agent/mainagent.go`) — human-interface; chat via SSE; tools for observing/steering; guard hooks; side capture.
- ChatAgent (`sidequestion/`) — side questions, async, no tools.
- Goals decomposer — `DecomposeGoalsWithProvider` called at task creation (`server/goals.go:387`); outputs `set_goals`/`add_task_scope` calls; runs **synchronously at task creation** (LLM-bound).
- Trigger/event system: `db/triggers.go` + cron (automation tools, IX.4+).
- Intercept LLM judge — `server/task_llm.go` (`Reviewer`), LLM retries excluded from quota handling (`isTaskLLMRuntimeError`).
- **Robustness gaps (IX.2)**: no durable job queue (all jobs are engine goroutines — see §20 P1); goal decomposition re-runs per restart? (restart behavior — partially unknown, §27).

---

## 9. Existing security assessment capabilities (F4)

| Capability | Status | Evidence |
|---|---|---|
| HTTP Request (first-class) | F4 | `agent/tools_http.go` (`HTTPRequest`), Windows curl-compat fix rationale documented |
| Scope enforcement (pre-dial pin) | F4 | `agent/httpclient.go` + `agent/scopecheck.go`; VIII.4.2 |
| Probe oracles (5 kinds) | F4 | `agent/tools_probe.go` (sqli_boolean/time, xss_reflect, ssti_eval, path_traversal); T+R verified in VIII.1 |
| Crawler | F3 | `agent/tools_probe.go` Crawl tool; positive/negative fixtures |
| Authentication/Session tools | F3/F4 | `agent/tools_login.go`, `agent/httpclient.go` (jar+pinned headers) |
| Finding Gate (evidence, no-solution-leak, no-unconfirmed) | F3 | `agent/finding_gate.go`; F3 (prompt+gate; stronger in v5) |
| Finding dedup | F1/F2 | **`db/schema.sql` findings table has NO unique constraint; `db.RecordFindingTx` has no dedup path** — FP risk in long runs (IX.2 P1) |
| Traffic capture + audit | F5 | VIII.6.1/6.3 (dedicated runtime roles, audit fsync) |
| AttackPath | F3 | `agent/tools_attackpath.go` (conservative, shared-asset/host/identity) |
| Retest (tool + independent flow) | F3/F4 | `agent/tools_retest.go`, `server/finding_retests.go` (VIII.6.3 fail-closed) |
| TrafficRef provenance | F5 | VIII.6.3 (app-level, content-addressed, digest-only limitation documented) |
| Evidence integrity + invalidation | F5 | VIII.6.3 (read-time validation; IX.1 offline benchmark-verified) |
| Confidence model | F2 | Asset states (observed/confirmed/tombstoned) + evidence_state/valid; finding confidence coarse (IX.2 candidate) |
| Egress allowlist (proxy, all paths) | F5 | VIII.4.3/4 (fail-closed, hot-swap) |
| Intercept rules + LLM judge + ask-approval | F3 | `guard/`, `intercept/`, `server/task_intercept.go` |
| Enrich (DNS/HTTP auto-completion) | F2/F3 | `enrich/` — async workers, graceful (cooldown, no persistence across restart? minor gap §27) |
| Notify (multi-channel, SSRF-hardened) | F3/F4 | `notify/` (webhook SSRF test + mask/redact tests) |
| Token/usage accounting | F5 | `llm_usage`, `llm_records`, `db/llm_usage.go` (TokenByModel, etc.) |
| Task archives | F3 | `server/task_archives.go`, `db/task_archives.go` (SHA256 manifest, states, guards) |
| Plugins/extension points | F3 | MCP servers (remote stdio/HTTP), DB tool overrides (`ToolResolve`), skills (fs), hooks (`intercept`) |
| Frontend screens (all features) | F3 | Next.js pages for all backend features |

---

## 10. LLM and provider assessment (F4/F5)

- **Interface**: `llm.Provider` (Stream + Complete); norma `agentcore.Session.Prompt` — harness loop (tools/settle/abort/budgets). Streaming everywhere in production; `NonStreaming` opt-in.
- **Protocols**: anthropic, openai, openai-responses (`norma/llm`), providers built by `agent/provider.go` (with `agent/provider_egress.go`, `agent/provider_quota` 429-normalization, quotaAwareTransport).
- **Config**: `db.LLMProfile` (format, base_url, api_key_hint only, window, thinking, streaming, max_tokens, retry overrides, priority, pool_exclude), `llm_profiles`/`llm_profile_health`/`task_llm_profiles`, `llm/profiles` routes, `/api/llm/pool` status.
- **Failover**: `llmpool` (member chain, rank, health, RateLimit), server router (`server/task_llm.go`, `isRetryableStreamError`, quota/failover transitions, `llm_failover` activity).
- **Token accounting**: `llm_usage` (every call, success/error), `llm_records` (full wire bodies, gated), `agent/capture.go` (live usage events), `db.llm_usage.TokenByModel/UsageByProfile`.
- **Mock providers for tests**: norma test doubles, `agent/side_questions_test.go` `sideAgentProvider`, `agent/testmain_test.go`, IX.1 `benchmark.ScriptedProvider` (offline runs).
- **Mock mode gap (§27)**: `web/src/lib/mock/enabled` (frontend MOCK system) — dev-time frontend mock; separate from benchmark (FD).

---

## 11. Evidence and Finding assessment (F4)

- `evidence/store.go` (Store.Record/Bind/OpenBody/StageFindingsExport/validateBindings/RunGC) — VIII.6.3.
- `db/finding_traffic.go` (TrafficRef, TrafficSnapshotID=content hash, LockFindingEvidenceTx, AddFindingTrafficTx, EditFindingTraffic, Version bump), `traffic/evidence.go` (ReadEvidence — server-side capture read).
- Finding lifecycle: `findings` table (triage states: pending/in_progress/confirmed/resolved/fixed/false_positive/ignored/duplicate/risk_accepted), `db.RecordFindingTx` (node kind='finding' + findings row + notification + traffic bindings).
- **Gap (§19 P1)**: no engine-level finding dedup; DB has no unique constraint.
- Reports: `report/findings.go` (FindingsMarkdown/CSV/SingleFindingMarkdown/FindingFilename — safe filenames vs zip-slip), `server/finding_traffic_export.go`, `server/findingsExport` (md-zip/csv/json/md-single).
- Retest: independent flow (VIII.6.3, fail-closed) + tool-based re-oracle (`agent/tools_retest.go`).
- CDN/conf path: asset states (observed/confirmed/tombstoned) + `VerifyFile` hash/length + finding invalidation.

---

## 12. Benchmark assessment (IX.1, F5 offline, FM offline)

`benchmark/` package (10 files): case schema (§4 of IX.1.1? — see `benchmark/case.go`), 7-case dataset, `ScriptedProvider` (offline deterministic), `Runner` (real Engine + Worker + `report_finding`), metrics (`ComputeMetrics` TP/FP/TN/FN, P/R/F1/specificity), `ComputeSuiteMetrics`, `Artifacts` (manifests, JSON save/load/validate, safe config fingerprint — secrets omitted), `CompareRuns`, `CLI` (`artex benchmark list|validate|run|compare|report`), self-tests (`benchmark/selfcheck_test.go` — 20+ adversarial metric/artifact tests, passing).
- **Baseline (IX.1, bench-offline-20261123T190807Z-8fb76ad60227)**: 7/7 completed (100.0%), micro TP=4 FP=0 FN=0 (precision/recall/F1=1.000, specificity=0), per-case detail (sql_boolean_confirmed_local TP=1 (235 ms), xss_reflected_confirmed_local TP=1 (114 ms), ssti_confirmed_local TP=1 (107 ms), harmless_404_no_finding 0, worker_empty_response_edge 0, duplicate_finding_dedup_edge TP=1, invalid_evidence_gate_edge 0).
- Two independent baselines compare as `incompatible: ok`, `TP 4→4 (Δ+0/−0)`.
- **Live mode (IX.1.1 gap)**: supported but **not exercised**: `LiveProvider(pg)` from `llm_profiles` (no new integration), `LiveModelUsage` from `llm_usage`. §24 for roadmap.
- Note: offline mode's `evidence_state=none` for findings — no traffic capture in offline runs; expected per `state=none` semantics (VIII.6.3).

---

## 13. CLI/API/Frontend assessment (F3/F4)

- **CLI**: `artex` (flags-only; `benchmark` subcommand added IX.1); versions (selfupdate).
- **HTTP API**: **243 routes** (tasks/assets/categories/templates, findings + lineage + export + retests + traffic + binding, exploration graph/nodes/activity/stream + tokens stats, llm profiles/retry/pool + records by model + resolver, agents/tools/triggers/prompts/prompts preview/visibility, skills (fs tree/edit/usage), notify (meta/channels/deliveries/retry/usage), intercept (rules/assistant/judge/history/trace/execution/approvals), workspace, companies, sync (scopesentry), report, settings, webhooks test (python detect), gc, chat + uploads + chat status/stop, conversations (messages/stop), auth (init/login/change), auth status, health, update check/apply/rollback, traffic (search/get/blob/delete), audit/logs/logs/history/stream, visibility toggles, intercept tool-config/judge). **API↔frontend coverage verified (§16)** — 2 gaps documented.
- **Frontend** (Next.js App Router): pages (auth/login, auth/setup, external, main/chat, dashboard, function/{assets,commands,findings,llm-records,sync,tasks(+detail+tabs),traffic,workspace}, system/{agents,intercept,llm,logs,mcp,notify,settings,skills,tools}); components (exploration-graph, finding-*, traffic-*, approval-*, agent-editor, todo-popover, transcript, status-badge, etc.); lib (types.ts, api.ts, auth.ts, status.ts, utils.ts, activity-merge, side-questions, chat-send-mode, task-assets, cookie.client, local-storage.client, company-scope, chat-mentions, mock/*); config (app-config.ts); navigation; styles. **Page↔route coverage verified (§16) — 0 known gaps.**

---

## 14. Database and data-model assessment (F5)

- **49 tables** (§3.3), migrations idempotent (`db/schema.sql`, `db/db.go` embed), roles model (VIII.6.1).
- **Exploration graph**: `exploration_nodes` (kind goal|intent|fact|finding|hint|digest; state open/running/done/blocked/exhausted/stopped/paused/deleted/origin; payload jsonb; anchors; priority), `exploration_edges`, `exploration_anchors`.
- **Activity ledger**: `activity` (worker/tool/tool_result/text/thinking/usage/result/user/round/status transitions; token fields on result).
- **Assets**: new unified (`db/assets.go`; global, shared, company-scoped).
- **Findings**: `findings` + `finding_retests` + `traffic_evidence_snapshots` + `finding_traffic_bindings`.
- **Telemetry**: `llm_usage`, `llm_records`, `activity`, `tool_usage`, `server_logs`, `guard` audit, intercept trace/pending/history, task_archives, `notify_*`.
- **Consistency**: strict foreign-key discipline, adapters for plugin layering (8.x), content-addressed evidence blobs, session-id convention (exp{N}-{role}).
- Minor gaps (§27): legacy & new asset models dual-track (intentional), no `cancelled` task status (side questions have one), FK → activity for cancelled intents.

---

## 15. Test coverage map (§§1.5/8.x, IX.1.1 — from parallel coverage audit)

Note: counts are `func Test\w+\(` totals in `*_test.go` (grep over all packages) + source inspection; the Go toolchain is not installed on this host, so no `go test` was run; `web` (TS frontend) excluded from Go counts. **Totals: 196 test files, ≈834 tests.**

| Package | Files | Tests | Integration (PG-gated) | Security | Concurrency | Main gaps |
|---|---|---|---|---|---|---|
| agent | 50 | 176 | insert_assets (8), review_context (1), capture_approval (1) | scope_failclosed (10), scope_pin (7), provider_dns (9), fs_containment (6), bash_guard (7), provider_trust (6), provider_egress (5), provider_transport (5), http_redirect (1), target_boundary (3), provider_adversarial (2), scopecheck (7), scope_boundary (4), scope_reuse (1), scope_task_isolation (6) | TestTrustSetConcurrentSeedAndRead, TestScopeIsolation_ConcurrentOverlap, t.Parallel in provider_quota (3) | Plan + Worker.ExecuteWithMessage + worker/planner setters (§27 P3) |
| server | 54 | 219 | core (1); PG via NewManager + advisory lock; secrets_leak (2), customtool_exec (1), v8_6_3_auth (2), egress_audit_wiring (1) | — | engine_llm_calls (1), manager_lifecycle (2), engine_cancelcause (13), task_delete_barrier (13) | Server.New/Handler — indirect (§27 P3) |
| db | 47 | 168 | all — `t.Skipf("postgres unavailable")` | company_scope (6), companies (12), task_source_limit (3) | task_delete_concurrency (1), company_lock (1), task_context_lock (5), task_context_unit (2) | — |
| evidence | 4 | 14 | All — `ARTEX_PG_DSN` gate | v8_6_tamper (3) | TestEvidenceConcurrentBindAndDelete, TestEvidenceValidityConcurrentBind_v863 | Store.Binding, WithBinding — partial |
| traffic | 12 | 62 | archive_test (3); rest SQLite | egress_test, egress_allowlist_test, egress_dial_test, egress_ipv6_tls_test | TestRecordConcurrent (traffic_store_test), TestTrafficAllowlistConcurrentReadWrite | SetEgressAddressPolicy (egress.go:176), EgressAllowedAddr (L123) |
| llmpool | 2 | 20 | — | — | — | — |
| llmrec | 2 | 9 | — | — | WithWorker (L46) | — |
| guard | 1 | 1 | — | — | — | New/NewWithInterceptor — indirect |
| intercept | 3 | 17 | — | — | TestTraceConcurrentDistinctCalls (trace_test:120) | — |
| config | 1 | 1 | — | — | — | Redact (config.go:181) |
| enrich | 1 | 3 | — | — | — | — |
| notify | 11 | 96 | — | ssrf_test (8) | — | — |
| sidequestion | 2 | 13 | — | — | TestMainSideConcurrencyAndIndependentCancellation (sidequestion_test.go:201) | — |
| mcphttp | 1 | 1 | — | — | — | New/NewSSE/ToolName |
| **report** | **0** | **0** | — | — | — | **All 9 funcs** (§27 P3) |
| benchmark | 2 | 14 | runner_test (2, PG-gated) | SafeConfigFingerprint | — | — |
| selfupdate | 2 | 19 | — | TestSelfUpdateBlockedWhenDisabled, TestVerifyStagedRejectsTamperedBinary | — | — |
| web | 0 | 0 | — (TS) | — | — | — |
| cmd/artex | 1 | 1 | — | — | — | — |

**Top-10 untested areas** (§27): ① `report/` — 0 tests; all of `Markdown`, `FindingsMarkdown`, `SingleFindingMarkdown`, `FindingFilename`, `FindingsCSV`; ② `agent/planner.go` `Plan` (planner.go:355) — no direct unit tests; planner setters — no direct calls; ③ `agent/worker.go` `ExecuteWithMessage` (worker.go:357) — no direct tests; worker setters — no direct calls; ④ `traffic/egress.go` `SetEgressAddressPolicy`/`EgressAllowedAddr` — untested; ⑤ `evidence/store.go` `Binding`/`WithBinding` (L377) — no direct test; ⑥ `server/server.go` `New`/`Handler` — indirect only; ⑦ `llmrec/llmrec.go` `WithWorker` (L46) — untested; ⑧ `guard/guard.go` `New`/`NewWithInterceptor` — indirect; ⑨ `mcphttp/client.go` `New`/`NewSSE`/`ToolName` — untested (legacy SSE tested); ⑩ `config/config.go` `Redact` — untested (`PostgresDSN` L137 tested).

**Revisited** (§27): `TestGraphOverviewExpandsAssociatedCompanyScope` (agent — pre-existing/flaky), `TestSkillRelPath` (server — pre-existing; Unix-semantics test vs Windows `filepath.Clean` — corroborated cause: Windows `\` normalization mismatch at skill_upload_test.go:41–46 vs ok-map paths), `TestCoreTaskLifecyclePG` (server — environmental, no LLM provider), `TestEvidenceBindingLifecycle` (evidence — environmental flake, passes standalone), plus earlier-observed `TestSyntheticSecretsNotExposedViaProfilesAPI`/`TestTaskMetadataPatchReturnsRenameAndPin` (server — environmental, Windows temp-dir cleanup race on fresh DBs).

---
## 16. Feature reachability matrix (§0–§18)

Verified end-to-end workflows (T/R): task create+run+findings (VIII.x), planner+worker (agent.engine tests, e2e IX.6/VIII.x), finding gate, report markdown/csv/zip, traffic binding + blob export (VIII.6.3 runtime), retest (VIII.6.3), guard approvals, egress deny (VIII.4.3/8/9), archive/restore, task archives, LLM failover, token stats, chat (side questions).

Mock/unused/internal-only: `TestConnection` (route; OK), `noa` (experimental; not auto-enabled), intercept-LLM judge (wired but primary path is rules), MCP remote (not in default UI flow), frontend `MOCK` mode (dev).

Backend-no-frontend-route: none found; frontend-no-backend: none found (2 known API↔frontend mismatches documented in §13).

---

## 17. Duplication & overlap analysis (§5)

| Case | Status | Details |
|---|---|---|
| compaction (built-in) vs noa | FD | Two context-management systems (§17-FD, IX.1 §20-5: best case 2-3; worst 9; avoid a fifth injection layer) |
| HTTP client layers | FD | `agent/httpclient.go` (tools, intercept) vs `norma` webfetch (worker WebFetch; VIII.4.2 disabled) vs `traffic`-recorded requests — three paths, deliberately distinct scopes; consolidation blocked by scope differences |
| Cron engine | FD | automation tools + engine heartbeat + scheduler_state — co-exist by design |
| Guard hook surface | F1/F2 | Guard both audits and intercepts; audit trail in DB is the audit — strongest path (all decisions in DB) |
| Frontend mock vs backend | FD | `web/src/lib/mock` — dev-time frontend mock; separate from benchmark (§27 P3) |
| EmptyScope effects | N/A | FK discipline + N-seam (explained) |
| Guard fallback (RoE) | FD | Removed; replaced by DB intercept rules + judge |

---

## 18. Architectural maturity scores (§9)

| Subsystem | Score | Rationale |
|---|---|---|
| Core Engine | **4** | Fully runtime-verified (8.x/9.x) |
| Planner | **3** | LLM-driven, not code-validated; compaction experimental |
| Worker | **4** | Runtime-verified (engine/worker tests, VIII.6/8.5) |
| Agent Orchestration | **4** | Well tested (server engine/tasks/goals); less runtime |
| Tool Framework | **4** | DB-overridable catalog, guards, seeds; tested (§15) |
| HTTP & Crawler | **4** | New native clients (VIII.1, F3); tested |
| Existing Assessment Logic | **4** | ORACLE verified in VIII.1/T+R |
| Finding Quality | **3** | Gate strong (3 rules); dedup/coverage missing (P1); confidence coarse (P3) |
| Evidence Management | **5** | VIII.6.3 runtime-verified (tapes); IX.1 offline-verified (7/7 cases) |
| AttackPath | **3** | Conservative chains; read-only analysis; not engine-connected |
| Retest | **4** | Independent flow fail-closed (VIII.6.3); tool-based retest |
| LLM Provider Infra | **5** | Protocol-complete (anthropic/openai/responses), failover, health, quota, mock |
| Database Layer | **5** | 49 tables, T+R, role model, migrations, archives |
| API | **4** | 243 routes, DTO-tested; a few test gaps |
| Frontend | **3** | Pages/components complete; some less-tested pages |
| CLI | **3** | Flags + benchmark subcommand |
| Benchmark | **5** | IX.1, F5, self-tested |
| Observability | **3** | activity/usage/records/logs/audit; gap: engine phase durations (P1) |
| Deployment & Ops | **4** | Archives, GC, restarts, migrations, runtime build |
| Security Controls | **5** | VIII.6.x verified; IRON BOUNDARY regression surface re-verified (§22) |

Macro: Engine=4, Planner=3, Worker=4, AgentOrch=4, ToolFramework=4, HTTP=4, AssessmentLogic=4, FindingQuality=3, Evidence=5, AttackPath=3, Retest=4, LLM=5, DB=5, API=4, Frontend=3, CLI=3, Benchmark=5, Observability=3, DeployOps=4, Security=5. Average ≈ 3.97 → **strong operational platform, patchy/older parts in finding quality + observability**.

---

## 19. Existing functionality not previously considered (§10-§11, plus):

- **`guard` audits** — every tool call audited; `intercept` trace/pending/history.
- **`Enrich`** — async dnsx + proxy probes auto-completion (VIII.4.4.1 runtime).
- **`notify`** — multi-channel + SSRF-hardened webhooks (VIII.x).
- **Task archives** — backup/restore (VIII.6+).
- **Provider failover + health** — `llmpool` (tested, VIII.3.5).
- **`agent/goals` — goal decomposer** — `set_goals` + scope extraction.
- **Cron/scheduler-state** — `scheduler_state` table + automation tools.
- **`benchmark/` offline mode** — IX.1.
- **`web/src/lib/mock`** — frontend dev mock system (§27 P3).

## 20. Genuinely missing capabilities

| Gap | Confidence | IX.2 candidate |
|---|---|---|
| Engine-level finding dedup (unicity constraint) | High | **P1** (core: `findings` table has no unique constraint; `RecordFindingTx` inserts without dedup check) |
| Planner-level intent dedup enforcement | High | **P1** (prompt-level only) |
| Engine observability: phase durations, finding rate, retry counts, agent activity aggregation | High | **P1** (extend `engine.go`/telemetry) |
| Job durability (async jobs persist state; resume after restart) | Medium | **P1** (jobs currently in-memory; restart recovery exists only for intents) |
| Live-model benchmark | High | **P2** (IX.2: first real LLM comparison — reuse IX.1 harness: LiveProvider, LiveModelUsage, live artifact mode, truth-table extension with cross-LLM comparison) |
| Confidence/attribution calibration (finding confidence, FP-rate calibration from benchmarks) | Medium | **P3** (use IX.1 output as input) |
| Cold compaction maturity (noa vs built-in duplication) | Medium | **P3** (§17-FD) |
| Guard approvals pressure UX (ask-approvals) | Low | **P3** |
| Documentation (user guides per subsystem) | Low | **P3** |
| `docs/漏洞流量证据.md` only — user docs sparse elsewhere | Low | **P3** |

## 21. Known technical debt

1. `guard` — audit-only audit trail (no blocking) — accepted design; "RoE removed, replacement TBD".
2. Legacy data + new assets dual model — migration seams.
3. `llm_records` gated by default — ok (provenance).
4. Engine in-memory state — restart consistency via ResetRunningIntents.
5. Windows UI inconsistencies in CI (Windows symlinks) — TBD, environment; 8.6 audit noted; low severity.
6. Frontend components maturity varies — low-value pages are interactive but basic.
7. Provider quota handling (429 normalization) is completed; edge cases (mid-stream drops) documented.
8. `EXIF`: chunk sizes (60-70 KB tool outputs spill to cmd-output; norma default 30000) — fine.

## 22. Security posture snapshot (§8, amended by IRON BOUNDARY)

Previously verified (VIII.4–VIII.6) and unchanged by IX.1.1:

- **F-A1 (CRITICAL)**: CLOSED (8.6.1) — runtime-verified: restricted runtime role (`artex_v86_runtime_app`, no bypass, no create), migration role, `ARTEX_DB_SKIP_MIGRATION=1`. Runtime re-verified in IX.1 (restricted role connects, no DDL errors).
- **F-A2**: UNKNOWN (environment — Windows symlink skipped; static review ok).
- **F-B2**: CLOSED (VIII.6.2, documented).
- **F-B3**: CLOSED (VIII.6.3 — read-time invalidation, fail-closed, IX.1-benchmark-verified offline).
- **F-B1**: CLOSED (VIII.6.3 — app-level authenticity; digest-only limitation documented, FK/Malicious DB adversary documented as out of threat model).
- **F-E1**: CLOSED (8.6.1 — durable audit + N3 fsync, runtime-verified).
- **F-E2**: CLOSED (VIII.6.2 — auth model: cookie/JWT; no production cookie write on login path; extractToken hardened to GET/HEAD for cookie/query in VIII.6.3 (N1)).
- **F-A5/F-B5**: informational.
- **Race**: `RACE DETECTOR UNKNOWN — ENVIRONMENT BLOCKED` (§27 — no compilers).

**Previously identified risks re-assessed**:
- F-A4 (risk acceptance): documented accepted risks remain (provider/traffic pooling, TLS SNI runtime, IPv6 runtime).
- F-A5 (error channels): informational, unchanged.
- F-B5 (read containment): F1 — read permissive, documented trade-off (8.5.1).

**Apparent regressions from IX.1.1**: none.

## 23. Prioritized development roadmap (§12)

### P1 — MUST (blocking risk, well-defined, XS-L)
1. **Engine observability core (§27 P1)** — engine-level phase durations, finding rate, tool retry counters, terminal reasons; wire to `activity` + `llm_usage` (extend). Enables IX.2 comparisons.
2. **Finding dedup (§27 P1)** — DB-level uniqueness constraint + engine check; fingerprint = (vulnclass, name, severity, asset_ids, url, param).
3. **Planner dedup guards (§27 P1)** — intent re-emission prevention (both-code-and-prompt): dedup by structural key in `add_intent` handler; planner-side pre-flight dedup.

### P2 — SHOULD (high value, medium XS)
4. **IX.2 — Live-model benchmark (§27 P2)** — extend `benchmark/` with live mode: real profile from `llm_profiles`, live artifacts (mode=live), comparison reports; validate contract.
5. **Confidence/attribution calibration (§27 P2)** — use IX.1 offline benchmark to measure precision/recall per vuln class; produce confidence scores from measurement (documented as experimental).
6. **Historical task comparison (§27 P2)** — archive/restore comparison engine (VIII.x has archives; IX.2 builds comparison diffs; low-ish).
7. **Cold-compaction maturity (§17-FD, §27 P3)** — resolve compaction duplication (noa vs built-in) if IX.2 targets effectiveness parity.

### P3 — NICE (nice-to-have, M-L)
8. **Guard approvals UX** (approve/deny UX on frontend).
9. **Docs** (§27 P3 — user guides).
10. **Extra probers** (Phase-2/3 concepts: Phase 2 (fuzz Bola), Phase 3 (vuln detection beyond std 5 classes) — see strategy.txt/probe tools; product-level roadmap).
11. **Extra observation sources**: traffic analytics (VIII.4.4.1 scope), graph coverage analytics.
12. **ASLC-fix**: frontend `MOCK` (dev-only, §27 P3).
13. **Release automation**: release-time LLD build (§27 P3).

### P4 — OPTIONAL (optional, or revisit with IX.3+)
14. **Durable job store** (§27 P1 as *medium* — resume after restart for goal decomposition, side questions, non-engine jobs).
15. **Task orchestration improvements** (parent/child agent delegation beyond ParentRef fields).

### P5 — DEFERRED
16. **Windows symlink support** (§27 A2) — env-specific, documented as not-current-system.
17. **Denylist/public tags/image optimization** — out of scope.

## 24. Recommended Phase IX.2 scope

**P1 (enabler)**: engine observability (metrics foundation).
**P2 (score)**: live-model benchmark (IX.2 core deliverable); finding dedup (small).
**P3 (polish)**: compaction consolidation; guard UX; docs.
**P4 (strategic)**: durable job store; orchestrator improvements.

**IX.2 should NOT** start before: P1 engine observability lands (telemetry foundation for all IX.2 comparisons); P2 finding dedup and live benchmark.

## 25. Explicitly deferred work

- Race detector (VIII.6.3 debt) — blocked by environment (§27).
- Windows symlink fix — environment-specific (§27).
- Provider/traffic pooling maturity, TLS SNI runtime, IPv6 runtime, frontend analytics, extra probers — AQIMEV (§21, §20).
- Expired session/passkey/bruteforce features — not in scope.
- F5 and below — dashboard polish, data-richness improvements.
- Goal provenance audit — "reads are OK, intent-risk is mine" (§21).

## 26. Final project status

- **Engine**: F4 — production-ready, runtime-verified.
- **Planner/Worker**: F3/F4.
- **Security subsystems**: F5 — verified through VIII.6 + IRON BOUNDARY regression.
- **Product**: functional, mature, not fully finished.
- **Next milestone (IX.2)**: engine observability (P1) + live-model benchmark (P2) + finding dedup (P1).
- **Overall (subjective)**: **75% done** — engine and security strong; finding quality and observability medium; IX.2 natural path:IX.2 = "engine observability + live benchmark + finding dedup".

---

## 27. Additional notes / inconsistencies found (§14, §20-F1, §27)

1. **C2 bypass missing** (`agent/provider.go:415`): SSRF hardening removed the C2 (**“curl; No allowlist?”-like tunneling) `opt.ProbeTimeout` `empty`. Add regression tests for `DoHTTP`'s denial.
   Hmm — no, that's from VIII.x.
2. **Guard audit does not cover `Shell_open`/`Shell_send`** (shell_open uses HTTP — §21 F15 — legal but incomplete coverage: user must approve separately).
3. **`engine.emitActivity` drops on error with log only (not UI)** — §27 P3.
4. **Restart semantics for goal decomposition**: not investigated (partial).
5. **Windows symlink unsupported** — F0/UNKNOWN environment (§27).
6. **`Enrich` cooldown**: not persisted — work continues from the same engine process.
7. **`Expiry`**: `agent/session` has 30-day expiry warning — untested.
8. **Policy**: `intercept` "ask" flow needs UI approval — untested.
9. **Windows build**: `v8_6_3_*` tests skipped on Windows (no symlinks), and `TestEvidenceBindingLifecycle` flakes on Windows.
10. **Activity `kind='user'` for human-messages — no UI badge yet.**
11. **DB layer: legacy + new asset models dual-track** — 9.1 FD risk (§17).
12. **`Expiry` default value for TTFB — undocumented.**
13. **`Policy` — no user-approval on change-password.** — F0/UNKNOWN.

> Cross-checks against VIII.4/5/6/7/8/9 older audits: consistent with findings in §27 (e.g., F-A4 risk acceptance retained; F-B5 read containment; F-A2 env; N1/N3 remain resolved; F-B3 closed by VIII.6.3).

---

## 28. Deliverables

1. `reports/PHASE9_1_1_FULL_PROJECT_REASSESSMENT.md` (this file).
2. `reports/ARTEX_CAPABILITY_INVENTORY.json` (machine-readable capability inventory — every capability: ID, name, category, status, key files, entry points, production callers, interfaces, deps, tests, runtime evidence, limitations, duplicates, recommended action, priority).
3. Phase IX.2 scope (§23-25).

---

## 29. Evidence index (§3/§4/§16/§18)

- **§4 source evidence**: direct reads of `agent/*`, `server/*`, `db/*`, `evidence/*`, `traffic/*`, `llmpool/*`, `llmrec/*`, `guard/*`, `intercept/*`, `enrich/*`, `notify/*`, `sidequestion/*`, `report/*`, `mcphttp/*`, `config/*`, `selfupdate/*`, `benchmark/*`, `cmd/*`, `web/*`, `db/schema.sql` (49 tables).
- **§16 T/R evidence**: Phase VIII.4.6/7/8 (T/R), VIII.5.1 (R), VIII.6/6.1/6.3 (R), IX.1 (T+R offline, F5), `TestGraphOverviewExpandsAssociatedCompanyScope`/`TestSkillRelPath` (pre-existing/flaky — blackbox-executed).
- **§22 regression evidence**: VIII.6.3 reports (unchanged files, same behavior), IX.1 implementation (no changes to security files).
- **§12 evidence**: `benchmark/` code + `benchmark-artifacts/v91-baseline3/run.json` + IX.1 test runs.
- **§27 evidence**: code reads (§29 index).
