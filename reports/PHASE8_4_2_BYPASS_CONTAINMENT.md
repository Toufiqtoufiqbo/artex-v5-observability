# PHASE8_4_2 — BYPASS CONTAINMENT (Phase F)

Date: 2026-11-21
Inspected paths: Bash (SDK), WebFetch (SDK), WebSearch (SDK), MCP (via `AugmentTools`).

Norma `agentcore`, `tool`, and `permission` packages are **external** (`github.com/Autumn-27/norma`) — no dependency modifications were made.

## 1. Bash (network-capable, unrestricted shell)

Inspection findings:
- Worker toolset built in `agent/worker.go:391-394` via `defaultToolsExcept("MultiEdit","Glob","Grep")` → includes the SDK's `actool.NewBash()` tool.
- Bash can reach the network through shells (curl, wget, ping, ssh, ...) — paths invisible to `doHTTP`/`scopeGuard`.

Containment implemented (`agent/bash_guard.go`):
- `workerDefaultTools()` replaces the `Bash` entry with `bashGuardTool` wrapping `actool.NewBash()`.
- `bashGuardTool.Call` rejects commands matching `bashNetworkPattern` with an explicit error; everything else delegates unchanged.
- Pattern: `(?i)(?:^|[\s/])(?:curl|wget|netcat|ncat|telnet|ping|ping6|nslookup|dig|whois|ssh|scp|sftp|rsync|nmap|masscan|bscan|iwr|invoke-webrequest|invoke-restmethod|invoke-webservice|openssl)(?:\.exe)?\b|/dev/tcp/`
  - Boundaries are `^`/whitespace/slash so quoted or concatenated tokens do not false-positive on ordinary commands.
- Known limits (documented): heuristic, not a policy engine. Not covered by design: `nc` (excluded due to false-positive risk in scripts), `powershell`/`cmd` inline network operations without the listed tokens, `python -c "import socket; ..."` (not tokenized), `curl` invoked via a full path with obfuscation.
- `TestBashGuard_RejectsNetworkCommands` (13 tokens) and `TestBashGuard_AllowsLocalCommands` (10 commands) pin the token behavior; `TestBashGuardTool_GuardDecisions` proves the guard rejects network and delegates local via the fake inner tool.

Note: `NewBash` runs via `PermissionModeBypass` in the worker; the guard is the containment floor. The deeper containment is a separate phase (documented below).

## 2. WebFetch (SDK, separate HTTP client via recording proxy)

Inspection findings:
- `agent/worker.go:471`: `EnableWebFetch: true` — the SDK WebFetch tool was enabled in the worker; its HTTP path goes through the traffic proxy, not `doHTTP`.

Containment:
- `EnableWebFetch: false` (worker).

## 3. WebSearch (SDK)

- `agent/worker.go:476`: `EnableWebSearch: w.webSearch.Enabled` — enabled by default.
- Containment: `EnableWebSearch: false` (worker).

## 4. MCP

Inspection findings:
- Worker toolset built by `AugmentTools` (`agent/assembly.go:39`); MCP/skill tools enter via `ToolResolve` (server/assembly.go:239) — tools with no `tools` row pass through.
- MCP tool names are `mcp__<server>__<tool>`.
- The traffic proxy (`agent/traffic`, `mcphttp`) records MCP traffic but does not validate destinations against task scope at this layer.

Containment:
- `agent/worker.go`: `tools = dropMCPTools(tools)` — all `mcp__*` tools are removed from the worker surface.
- `TestDropMCPTools` covers the drop logic.

## 5. Enrich (out of scope for this phase)

- `agent/enrich` performs async DNS/HTTP auto-completion for discovered assets. It is not part of the worker tool execution path and was not in this phase's mandates; documented here as a separate engineering item.

## 6. Status of "Disabled" per acceptance matrix

| Path | Status | Evidence |
|---|---|---|
| Bash network egress | Disabled (heuristic guard) | `bash_guard.go`, unit tests |
| Bash non-network commands | Still available (degraded) | guard delegates; `TestBashGuardTool_GuardDecisions` |
| WebFetch | Disabled | `EnableWebFetch: false` |
| WebSearch | Disabled | `EnableWebSearch: false` |
| MCP | Disabled (all `mcp__*` dropped from worker) | `dropMCPTools` + `TestDropMCPTools` |

## 7. Reversibility

Containment is a code-level decision (tool registration and worker options); disabling is not silent — errors and system prompt comments document it.

## 8. Known follow-up (documented as non-goal)

- Full network-command allowlist for Bash.
- DNS-to-dial pinning, proxy-level destination authorization.
- Intercept-framework approval flow for Bash network commands.
- Approval policy for MCP destinations.
