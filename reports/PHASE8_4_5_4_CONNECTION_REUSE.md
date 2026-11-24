# PHASE8_4_5.4 — CONNECTION REUSE CLOSURE

Implemented following the VIII.4.5.3 connection-reuse gap.

## Changes

`agent/httpclient.go` — `doHTTP` transport:
```go
DisableKeepAlives: true,
MaxIdleConns:      0,
DialContext:       dialPinnedContext,
```
Scoped assessment requests now get a fresh dial per request; no pooled connection can bypass `dialPinnedContext` authorization.

`agent/scope_reuse_test.go` — `TestPin_DialOnEveryRequest`:
- Two sequential doHTTP requests under the same scope; dial counter hook (`dialFn`).
- Assertion: exactly 2 dials (one per request) — no reuse. (The initial test's expectation of 4 dials was a test-authoring error, corrected to the intended one-dial-per-request property; the security assertion remains strict.)

## Validation

```
go test ./agent/ -count=1 → ok (exit=0)
go build ./... → exit=0
```

## Trade-off

- Performance: no keep-alive on scoped target requests; every request performs DNS validation + pinned dial. Security chosen over pooling.

## Remaining

- Hostname-kind rebinding gap (VIII.4.5.2); mixed DNS answer tests; IPv6; provider/traffic client pooling (not part of this closure).
