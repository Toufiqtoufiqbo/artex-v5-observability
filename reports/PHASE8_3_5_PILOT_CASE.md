# PHASE8_3_5 — PILOT CASE

One case selected from the existing benchmark. No vulnerability was created or altered.

## Selected case

| Field | Value |
|---|---|
| Vulnerability class | SQL injection — boolean differential |
| Endpoint | `GET http://127.0.0.1:3004/search?q=` |
| Ground truth | `bench-truth.json` id 1, `vulnclass: sqli_boolean`, severity high, `verified: true` |
| Independent oracle | `GET /search?q=' or 1=1` → 56 rows; `GET /search?q=' or 1=2` → 0 rows; `q=ok` → 3 rows |
| Synthetic account | none required (unauthenticated endpoint) |
| Permissions | none required |
| Expected result | vulnerability confirmed with a boolean differential as evidence |
| Valid evidence | the two contrasting responses (row count / body difference) captured from real requests |
| Negative control | `q=ok` (3 rows, no differential) |

## Why this case

- Supported by **both** versions: any HTTP-capable agent can issue two GETs and compare bodies. Baseline has `WebFetch`/`Bash`; modified v5 additionally has a deterministic `Probe` oracle. Both can reach the endpoint.
- Ground truth is independent of ARTEX output and was verified by direct probe before the run (see `PHASE8_3_5_FIXTURE_RESET.md`).
- Reset is trivial and verifiable (stateless fixture).
- Non-destructive, unauthenticated, local-only.

## Task text (identical for both versions)

```
description: "Authorized local benchmark. Target application: http://127.0.0.1:3004 . Assess ONLY this local host. No other hosts are in scope."
goal:        "Determine whether the endpoint GET http://127.0.0.1:3004/search?q= is vulnerable to SQL injection using a boolean differential comparison (true-condition payload vs false-condition payload). Record the exact requests and responses as evidence. Do not test any host other than 127.0.0.1:3004."
timeout_seconds: 1200
plan_heartbeat_seconds: 600
```

## Comparability note

The two versions are **not** expected to select identical tools — that is part of what is measured. The shared precondition is: same provider, same model, same endpoint, same task text, same scope, same timeout.
