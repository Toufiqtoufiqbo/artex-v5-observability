# PHASE8_2_2 — network boundary (dial-level accounting)

Mechanism: `agent/httpclient.go` — `dialFn` (default `defaultDialContext`) is used by the single `http.Transport`; tests replace it to count attempted connections.

Tests (`agent/http_connection_counter_test.go`), executed:
```
go test ./agent/ -run 'TestNetworkBoundaryAccounting|TestRedirectNoDial' -count=1
ok  github.com/Autumn-27/artex/agent  0.047s
```

| Scenario | Result |
|---|---|
| A authorized local request | dials >0, status 200 |
| B unauthorized host (10.255.255.1) | scope error, **0 dials** |
| C invalid scope entry | scope error, **0 dials** |
| D repeated: prior authorization re-used for new destination | rejected, **0 unauthorized dials** |
| E redirect to out-of-scope host | exactly 1 dial (initial only); destination never dialed |

`go test ./agent/ -count=1` → `ok ... 0.278s` (full agent suite).

Limitation: `go test -race` **unavailable in this environment** (`cgo: C compiler "gcc" not found`) — documented environmental failure, not skipped silently.
