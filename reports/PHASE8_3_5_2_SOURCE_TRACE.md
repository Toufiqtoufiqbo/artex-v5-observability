# PHASE8_3_5_2 — SOURCE TRACE (budget-limited)

**Verdict: `PARTIAL — BUDGET EXHAUSTED`**

## Scope and identities
- Baseline: `artex-global-main.zip`, SHA-256 `93AFBEC0…CD11E2` (8,944,269 B), extracted `%TEMP%\opencode\artex-base\artex-global-main`. Git metadata: none — commit identity UNPROVEN.
- Modified v5: `artex-original` @ `e6ec569`, dirty tree (tracked: `agent/tools.go`, `agent/worker.go`; untracked: `agent/finding_gate.go`, `agent/httpclient.go`, `agent/scopecheck.go`, `agent/strategy.go`, `agent/tools_http.go`, `agent/tools_probe.go`, `agent/tools_login.go`, `agent/tools_attackpath.go`, `agent/tools_retest.go`, `agent/tools_diff.go` + tests).

## What is SOURCE VERIFIED (from prior phases, same trees)
- Provider configuration endpoint: `server/server.go:1307 setLLM` → `agent.ConfigFrom(...)` (`server.go:1324`); `GET /api/llm` = `s.getLLM` (`server.go:737`); `POST /api/llm/test` = `s.testLLM` (`server.go:738`, impl ~1366).
- Engine provider construction: log line `[engine] built provider for LLM profile N (openai / nvidia/…)` originates from the engine/llmpool path (`server/llmpool.go`); worker provider build at `agent/worker.go` (Run/Execute).
- Both trees share the same provider interface (norma); baseline `norma v0.4.4`, modified `v0.4.3` — version skew.

## UNKNOWN / not traced this phase (budget)
Concrete HTTP client/transport for the provider request; exact dispatch/response/parse/consume functions with line ranges; Planner vs Worker provider selection precedence; `/api/llm/test` side-profile resolution path; retry/fallback implementation; response metadata (request ID/model/usage) extraction points. All marked **UNKNOWN** — no claims made.

## Minimum safe instrumentation (design only, not implemented)
Insertion points could not be established in this budget → `UNPROVEN`. Next phase must first read `server/llmpool.go` + norma provider client to locate the RoundTripper handoff and the response-parse return, then place observational hooks there.

## Next steps
1. Trace `server/llmpool.go` → norma provider → `client.Do` and response parse return.
2. Trace `agent/worker.go`/`agent/planner.go` provider selection.
3. Identify response metadata fields (request id/model/usage) in the parsed struct.
4. Only then specify exact instrumentation points.

No source was modified; no live inference; no runtime proof claimed.
