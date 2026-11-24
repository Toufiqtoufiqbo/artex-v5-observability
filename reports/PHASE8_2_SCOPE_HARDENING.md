# Phase VIII.2 — PHASE8_2_SCOPE_HARDENING

## Fix (P0)

`agent/scopecheck.go`: `case "company": return nil` → `case "company": continue` (fail closed). A company association alone no longer authorizes an arbitrary host.

`agent/scopecheck_test.go`: added `TestCheckScope_CompanyFailsClosed` — company-only scope must reject any host.

Commands and result:
```
go test ./agent/ -count=1
ok  github.com/Autumn-27/artex/agent  0.274s
```
All existing tests still pass (no regression).

## Acceptance status

- Valid in-scope / out-of-scope / missing / malformed / IP: covered by existing + new tests.
- Company: now fails closed — test added.
- Concurrent scope evaluation: NOT tested in this mission.
- Attempted bypass via redirects/DNS/retries: NOT tested in this mission (no mocked transport test implemented).
- "Unauthorized request never reaches network": structurally true for `doHTTP` entry (scopeGuard before client.Do), but not proven with a network-call counter.

## Modified files
- `agent/scopecheck.go`, `agent/scopecheck_test.go`.

See `PHASE8_2_FINAL.md` for the honest verdict and the remaining phases.
