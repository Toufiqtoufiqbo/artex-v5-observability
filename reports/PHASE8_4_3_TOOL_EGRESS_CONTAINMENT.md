# PHASE8_4_3 — TOOL EGRESS CONTAINMENT (Phase E)

## 1. MCP

- `dropMCPTools(tools)` — new (`agent/bash_guard.go`); removes any tool with prefix `mcp__` from the worker surface (`agent/worker.go:412`).
- Worker tools are built via `AugmentTools` → `ToolResolve` (server/assembly.go:239) — MCP tools enter here; the filter applies to the final worker tool list.
- **Verified** (not just registered): `TestDropMCPTools` — an `mcp__browser__navigate`-named tool is dropped; `Bash`/`WebFetch`/`Read`-named tools pass.

## 2. WebFetch

- `agent/worker.go:471`: `EnableWebFetch: false` (VIII.4.3; previously `true`).
- The WebFetch path goes through the traffic proxy via `WebFetchProxy` — disabled for the worker.
- **Status: DISABLED.**

## 3. WebSearch

- `agent/worker.go:476`: `EnableWebSearch: false` (previously `w.webSearch.Enabled`).
- **Status: DISABLED.**

## 4. Enrichment (`enrich`)

- `enrich/enrich.go` `buildClient`: dedicated `http.Client` with `InsecureSkipVerify: true`; `doDNS` (ResolveDomain) and `doHTTP` (ProbeSite); queue enqueued from `insertAssets`.
- VIII.4.3 decision: **documented limitation** — investigated, not claimed controlled. Changing this requires: a validated destination policy or disablement flag for the engine. Recorded in `PHASE8_4_3_FINAL.md` §6 and the acceptance matrix (`PHASE8_4_3_REGRESSION_REVIEW.md`).

## 5. Notifications

- `notify/http.go` — already has dial-level SSRF control (`blockInternalDial`); unchanged this phase. Follow-up: extend to target-assessment destinations when applicable.

## 6. Self-update

- `selfupdate/github.go` — own client for the release host; not wired to worker execution. Follow-up.

## 7. MCP-tool specifics

- MCP tool names are `mcp__<server>__<tool>` (`DeferredInfo.Deferred`/`GlobalNames`).
- `AugmentTools` returns MCP tools "untouched" when they have no DB row (server/assembly.go:260-262) — the worker filter applies regardless.

## 8. Evidence

```
go test ./agent/ -run 'TestDropMCPTools' -count=1 -v → PASS
```
