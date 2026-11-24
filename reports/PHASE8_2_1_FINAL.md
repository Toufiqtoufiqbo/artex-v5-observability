# PHASE8_2_1 — FINAL

**Verdict: `PARTIAL`**

## Completed (evidence in reports)
- Phase 1: company semantics verified — label only, fail-closed; test `TestCheckScope_CompanyFailsClosed`.
- Phase 2/3: redirect revalidation implemented in `doHTTP` (`CheckRedirect` with scopeGuard, 10-hop bound, credential stripping); test `TestRedirectScopeRevalidated` — cross-origin redirect rejected, same-origin allowed.
- `go test ./agent/ -count=1` → `ok ... 0.322s` (executed).

## Modified files
- `agent/scopecheck.go` (VIII.2: company fail-closed — preserved)
- `agent/scopecheck_test.go` (VIII.2 regression test — preserved)
- `agent/httpclient.go` (redirect guard added this phase)
- `agent/http_redirect_test.go` (new test this phase)

## NOT verified (criteria remain open)
Counting-transport proof of zero network attempts; DNS/multi-A/AAAA/IPv4-mapped; actual-connection pinning; retries; `go test -race`; concurrency/session isolation; retest FIXED/REGRESSED; benchmark reconciliation; full `go test ./...`; Phase 9 tests.

## Remaining deliverables (not created)
`PHASE8_2_1_REDIRECT_AUDIT.md`, `PHASE8_2_1_DNS_AUDIT.md`, `PHASE8_2_1_CONCURRENCY_AUDIT.md`, `PHASE8_2_1_SESSION_AUDIT.md`, `PHASE8_2_1_RETEST_AUDIT.md`, `PHASE8_2_1_BENCHMARK.md`, `PHASE8_2_1_REGRESSION.md`.

## Continuation instructions
1. Add injectable counting `http.RoundTripper` hook; prove rejected requests make zero connections.
2. DNS fixtures + connection pinning tests, then race tests.
3. Fixed/regressed bench variants; benchmark case reconciliation; full suites.

Stop condition applied: budget exhausted after P0 items 1 and 3; all changes compiling and tests passing; no partial code left applied.
