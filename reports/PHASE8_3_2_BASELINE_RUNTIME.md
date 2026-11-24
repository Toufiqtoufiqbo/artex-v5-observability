# PHASE8_3_2 — BASELINE RUNTIME

Isolated baseline: dedicated DB `artex_base` on PG 5433; ports 8888/8889; data dir in `%TEMP%\opencode\artex-base\data`.

Command (executed, with full Go path):
```
$env:ARTEX_PG_DSN='postgres://artex@127.0.0.1:5433/artex_base?sslmode=disable'
$env:ARTEX_ADMIN_PASSWORD='Baseline123!'; $env:ARTEX_LANG='en'
go run ./cmd/artex -addr 127.0.0.1:8888 -proxy 127.0.0.1:8889 -data <temp>/data
```
First attempt failed: `go: command not found` (PATH not inherited in background shell) — environmental, not a software defect; retried with full Go path.

Evidence:
- Port 8888 listening (pid 11080).
- `GET /api/health` → **200** `{"ok":true,"service":"artex","version":"dev"}`.
- `POST /api/auth/login` (ARTEX / Baseline123!) → **OK, token returned**.
- Startup log: `ARTEX dev backend listening on 127.0.0.1:8888 ... workers=3`; `no LLM provider configured — engine idle until set via /api/llm or env`.

Runtime smoke: **PASS** (process + bind + health + DB + login). No critical startup errors. Source identity limitation preserved: archive commit unverified.

Baseline left running on 8888 for continuation.
