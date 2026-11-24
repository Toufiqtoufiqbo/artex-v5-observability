# PHASE8_4_3 — SUBPROCESS CONTAINMENT (Phase D)

## 1. Implementation

- `agent/bash_guard.go` (VIII.4.2): `bashGuardTool` wraps `actool.NewBash()`; `workerDefaultTools()` installs it as the worker's `Bash`.
- Guard: `bashNetworkCommand(command)` — `(?i)(?:^|[\s/])(?:curl|wget|netcat|ncat|telnet|ping|ping6|nslookup|dig|whois|ssh|scp|sftp|rsync|nmap|masscan|bscan|iwr|invoke-webrequest|invoke-restmethod|invoke-webservice|openssl)(?:\.exe)?\b|/dev/tcp/`.
  - Word-bounded so quoted/concatenated tokens don't false-positive on ordinary commands (e.g., `grep -rn "ping" .` is allowed).
- `agent/bash_guard_test.go`: rejects 13 network tokens; allows 10 local commands; guard decisions via `fakeBashTool`.

## 2. Findings (inspection)

- `exec.Command`/`CommandContext` are not used directly in `artex-original` outside norma (grep: no production callers). The interactive shell family (`ShellSessionTools`) is opt-in and not enabled for the worker.
- `EnableWebFetch`/`EnableWebSearch` disabled in worker options (VIII.4.3).
- `dropMCPTools` removes `mcp__*` tools from the worker.

## 3. Known limits (Phase D, honest)

- Token heuristic — the phase requires **"disable autonomous network-capable subprocess execution"**; the guard is a containment floor, not full isolation:
  - `nc` excluded (false-positive risk).
  - Inline network in `python -c`/`node -e` one-liners not covered.
  - `cmd`/`powershell` inline network tokens beyond the list not covered.
- ShellSession tools (opt-in) not inspected to containment level — documented follow-up.
- No OS sandbox used (Windows has no reliable per-process egress sandbox here; installing privileged components is a non-goal).

## 4. Policy statement (Phase D)

**Until a subprocess execution environment can demonstrate enforceable network isolation, autonomous network-capable subprocess execution remains disabled** (guard active). Explicitly blocked operations return `Bash: network-capable commands are blocked during autonomous tasks (VIII.4.2 containment)`.

## 5. Evidence

```
go test ./agent/ -run 'TestBashGuard|TestWorkerDefaultTools_ContainsBash|TestDropMCPTools' -count=1 -v → PASS
```

## 6. Follow-up (separate phases)

- Full network-command allowlist.
- OS-level per-process egress isolation (Windows Job Objects / network namespaces where available).
- Interactive shell tools (ShellSessionTools) containment if enabled.
