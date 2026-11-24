# PHASE8_4_4 — INTERACTIVE SHELL & SUBPROCESS (Phase E)

## 1. Process creation paths (grep: `exec.Command`/`CommandContext` outside norma)

- **Bash tool** (`agent/bash_guard.go`) — new network guard in VIII.4.3; this phase extended:
  - The guard pattern was tightened to word-boundary-token form (Phase A doc in VIII.4.4).
  - The guard message now documents VIII.4.4 containment ("blocked during autonomous tasks").
  - Known limits recorded (see `SUBPROCESS_CONTAINATION`).
- **ShellSessionTools** (norma `tool/shell_session.go`) — opt-in, not registered for worker/chat defaults; not exposed in `workerDefaultTools()`; documented follow-up.
- `exec` outside the agent — grep shows no production callers in `artex-original` (only norma internal).

## 2. Enforcement status

| Mechanism | Status | Evidence |
|---|---|---|
| Bash tool | Guarded heuristic (VIII.4.3); limited | `TestBashGuard*` (VIII.4.3) |
| Python/Node inline network in Bash | NOT covered (heuristic) | documented in VIII.4.3 |
| Interactive shell (ShellSessionTools) | Not enabled by default; containment follow-up | code inspection (norma) |
| OS-level egress isolation | Not available (Windows: documented, no global firewall changes) | — |

## 3. Policy statement (Phase D)

Until a subprocess environment demonstrates enforceable isolation, autonomous network-capable subprocess execution is disabled/contained (Bash guard). OS sandbox integration is a follow-up (`SetNetworkEnabled`-style flag or per-run disable).

## 4. Follow-ups

- Full network-command allowlist.
- OS sandbox integration (Windows Job Objects/Network namespaces).
- ShellSessionTools containment if enabled.
