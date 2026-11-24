# PHASE8_4_3 — TEST RESULTS (Phase G)

Local-only: fake transports, controlled loopbacks, no public endpoints, no external provider calls.

## 1. Mandated commands (re-run; `backups\phase8_4_3\test-exit-codes.txt` — to verify)

```
go test ./agent/ -count=1                                → exit=0; ok github.com/Autumn-27/artex/agent
go test ./agent/ -run 'Scope|NetworkBoundary|Redirect'   → exit=0; ok
go test ./...    -run 'Scope|NetworkBoundary|Redirect'   → exit=0 (all packages ok)
go test ./agent/ -run 'TestGuardPanicConvertsPanicToToolError' -count=1 → exit=0; ok
go test ./agent/ ./traffic/ -count=1 -timeout 120s       → exit=0 (agent ok; traffic ok, 19.795s)
go build ./...                                           → exit=0
git diff --check                                         → exit=0
```

## 2. VIII.4.3 new tests

### traffic/egress_test.go (all PASS)

| Test | What it proves |
|---|---|
| `TestTrafficProxyEgressAllowlist` | Real proxy: allowed host reaches fixture and returns its body; non-allowlisted host denied (no success body). |
| `TestTrafficProxyEgressAllowlistEmptyRestores` | Empty allowlist permissive; case-insensitive; `host:port` entry matches; different port denied; subdomain denied; host-only entry matches any port. |
| `TestTrafficProxyAuditSink` | Audit events for allow and deny paths. |

### agent/egress_policy tests (VIII.4.3 audit)

- `TestEgressEventsEmitOnAllowAndDeny` — `doHTTP` to a loopback fixture emits allow event; denied destination emits deny event. (Implementation detail; see `PHASE8_4_3_EGRESS_AUDIT.md`.)

## 3. VIII.4.1 + VIII.4.2 suites re-run (PASS)

- VIII.4.1: all mandated commands PASS (see `PHASE8_4_2_REGRESSION_REVIEW.md`).
- VIII.4.2: `TestScopeIsolation*`, `TestBashGuard*`, `TestWorkerDefaultTools_ContainsBash`, `TestDropMCPTools` PASS.

## 4. Disabled/limited paths (Phase G matrix)

| Path | Status |
|---|---|
| Enrichment egress | DISABLED? — documented limitation (`PHASE8_4_3_EGRESS_INVENTORY.md`); follow-up |
| Self-update | follow-up (documented) |
| Interactive shell (ShellSessionTools) | follow-up (documented) |
| IPv6 (`hostOf`) | NOT VERIFIED, unchanged limitation |

## 5. Environmental notes

- `127.0.0.2`/`127.0.0.3` bindable → used for traffic proxy tests.
- `go test ./traffic/` takes ~20s (proxy startup + requests).

## 6. Secret scan

Strict scan over `reports/PHASE8_4_3_*.md` for an API key string of length ≥10, the admin password, or bearer-style credentials: **no matches** (the scan patterns themselves appear only as documentation text).
