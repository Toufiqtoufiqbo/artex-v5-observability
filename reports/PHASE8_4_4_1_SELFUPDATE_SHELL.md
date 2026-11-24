# PHASE8_4_4_1 — SELF-UPDATE + SHELLSESSIONTOOLS (Phase D)

## 1. Self-update — preserved (already Phase VIII.4.4)

`selfupdate/egress.go`: `IsNetworkEnabled()` default false; `FetchLatest`/`Stage` gate via `requireNetworkEnabled()` (returns explicit error before any dial). `SetNetworkEnabled(true)` is the explicit opt-in.

Tests: `TestSelfUpdateDisabledByDefault`, `TestSelfUpdateBlockedWhenDisabled` — PASS.

**VIII.4.4.1**: verified, no change needed; activation wiring is a follow-up if enablement becomes configurable.

## 2. ShellSessionTools (norma `tool/shell_session.go`)

Phase VIII.4.4.1 investigation (source):
- Registered only when interactive-shell agents are enabled (`agent.Options` / per-agent `interactive_shell`); not in `workerDefaultTools()`; not enabled in any default toolset used by server/manager.
- Jobs are spawned via norma's shell session manager; there is no server-side egress policy hook on interactive shells in this tree.
- Processes inherit environment (incl. `HTTP_PROXY` if set) — norma behavior, not inspected/modified here.

## 3. Required policy (as of VIII.4.4.1)

Until a contained variant exists, autonomous interactive-shell execution remains **disabled by default** (only registered for interactive agents — same posture as VIII.4.3). The containment proposal in `PHASE8_4_4_1_POLICY_INITIALIZATION.md` §D applies.

## 4. Files

- `selfupdate/egress.go` (VIII.4.4)
- `selfupdate/egress_test.go` (VIII.4.4)
- ShellSessionTools: no changes this phase.
