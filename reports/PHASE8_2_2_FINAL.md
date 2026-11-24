# PHASE8_2_2 — FINAL

**Verdict: `PARTIAL`**

## Completed (with runtime evidence)
- P0.1 network accounting: dial-layer counting implemented (`dialFn` hook); unauthorized/invalid/repeat requests make **0 dial attempts**; authorized request dials once (tests executed: `ok ... 0.047s`).
- P0.2 redirect enforcement: rejected redirect dialed only the initial host (exactly 1 dial), destination never contacted.
- Full `go test ./agent/ -count=1` → `ok ... 0.278s`.

## Modified files
- `agent/httpclient.go` — `dialFn`/`defaultDialContext`, transport `DialContext: dialFn`
- `agent/http_connection_counter_test.go` — new accounting tests
- (preserved) `agent/httpclient.go` redirect guard, `agent/http_redirect_test.go`, `agent/scopecheck.go` + test

## Not verified (criteria remain open)
Missing scope → DENY (empty scope still allows — unchanged, would break unscoped agents); retry authorization; connection-reuse bypass test; all execution paths inventory; concurrency/cancellation; transport/proxy audit; `go test -race` (env: no gcc); `go test ./...`.

## Remaining deliverables (not created)
`PHASE8_2_2_BASELINE.md`, `PHASE8_2_2_REDIRECT_BOUNDARY.md`, `PHASE8_2_2_RETRY_AUDIT.md`, `PHASE8_2_2_EXECUTION_PATH_AUDIT.md`, `PHASE8_2_2_CONCURRENCY.md`, `PHASE8_2_2_TRANSPORT_AUDIT.md`, `PHASE8_2_2_REGRESSION.md`.

## Continuation
1. Missing-scope default-deny policy decision + test.
2. Retry layer: revalidate destination per attempt; test.
3. Execution-path inventory mapping each tool → doHTTP.
4. Concurrency/cancellation tests; enable CGO/gcc for `-race`.
5. Proxy transport audit (ProxyFromEnvironment currently inherited — must be explicitly authorized/denied).
