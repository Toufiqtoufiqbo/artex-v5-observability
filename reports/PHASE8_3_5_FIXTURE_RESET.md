# PHASE8_3_5 â€” FIXTURE RESET

Fixture: `<WORKSPACE>\bench-vuln.py`
SHA-256: `832B19F401084774BDB6AFC6D45F20AFAB2A747C582673A7662DFEF65E2993F0`
Size: 5,252 bytes
Listen: `127.0.0.1:3004` (pid 11612)
Ground truth: `bench-truth.json` (6 positives), `bench-metrics-truth.json` (6 positives / 8 negatives)

## 1. State model

The fixture is **stateless**. The only writable artefact is `bench-hits.log` (append-only JSON lines, used for request counting). No database, no cookies persisted server-side, no files created by a run.

**Reset procedure:** truncate `bench-hits.log` to zero bytes. Nothing else changes.

## 2. Reset verification (executed)

```
BEFORE reset (hits log lines): 727
Set-Content -Path .\bench-hits.log -Value '' -NoNewline
AFTER reset (hits log lines): 0
```

## 3. Ground-truth probes after reset (executed)

| Probe | Observed |
|---|---|
| `GET /ok` | `200 ok` |
| `GET /search?q=ok` | `200` 3 products (negative control) |
| `GET /search?q=' or 1=1` | `200` 56 products (true condition) |
| `GET /search?q=' or 1=2` | `200` `data: []` (false condition) |
| `GET /greet?name=${7*7}` | `200 Hello, 49` (SSTI) |
| `GET /asset?file=etc/passwd` | `200 root:x:0:0:root:/root:/bin/bash` (traversal) |
| `GET /echo?q=<b>x</b>` | `200 <div id='o'><b>x</b></div>` (reflected XSS) |
| `GET /api/Users/10` | `200 {"id":10,"name":"alice","secret":"a1b2c3"}` |
| `GET /api/Users/11` | `200 {"id":11,"name":"bob","secret":"d4e5f6"}` |
| `GET /admin/stats` (no cookie) | `401 Unauthorized` (expected negative) |

All six ground-truth positives reproduce; negatives behave as documented.

## 4. Paired-run discipline

- Fixture reset (truncate hits log) immediately **before** each candidate run.
- Post-run inspection: count lines added to `bench-hits.log`, list distinct paths.
- Reset again before the second candidate.
- No state from run 1 can survive into run 2 because the app holds no mutable state.

## 5. Isolation

Baseline and modified use different databases (`artex_base_pilot` / `artex_pilot`), different ports (8888/8787), different data dirs, different log files. The fixture is shared but stateless, which is the only way to guarantee both saw the identical target.

**Reset reliability: VERIFIED.**
