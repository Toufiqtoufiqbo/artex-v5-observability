# PHASE8_4_2 â€” RUNTIME VALIDATION (Phase H)

Isolated runtime of the VIII.4.2-modified binary.

## 1. Identity

| Field | Value |
|---|---|
| Binary | `<WORKSPACE>\artex-v5-842.exe` (separate file; existing `artex-original-embed-v5.exe` untouched) |
| SHA-256 | `E39A54ABCBE84825C0B952CB7D53261B9D3B63FAF2807EB7A418D4AD4F14569E` |
| Build | `go build -o artex-v5-842.exe ./cmd/artex`, exit 0 |
| Source commit | `e6ec56999175509551a1e43a3ee3b053d0941bd9` |
| Go version | `go1.26.11 windows/amd64` |
| Database | `artex_v42` (fresh, created 21:47) |
| Data dir | `<WORKSPACE>\artex-v42-data` |
| Port / proxy | 127.0.0.1:8799 / 127.0.0.1:8798 |
| Log | `<WORKSPACE>\v5-842.log` |

## 2. Startup evidence

```
PID (listening): 10944
GET /api/health â†’ 200 {"ok":true,"service":"artex","version":"dev"}
POST /api/auth/init (<admin password, redacted>) â†’ 200
POST /api/llm â†’ 200; GET /api/llm â†’ 200
  {provider: "openai", model: "nvidia/nemotron-3.5-lightning-30b-a3b",
   base_url: "https://integrate.api.nvidia.com/v1", key_set: true}
Log: "[engine] no LLM provider configured â€” engine idle until set via /api/llm or env"
     "ARTEX dev backend listening on 127.0.0.1:8799 (data=..., workers=3)"
```

The existing service on 8787/8888 is **not** terminated or replaced.

## 3. Isolation

- Fresh DB `artex_v42` (old `artex`, `artex_base`, `artex_pilot` untouched).
- Separate data dir, port, proxy, and log.
- Fixture `bench-vuln.py` on 3004 unchanged and running (pid 11612).

## 4. Gateway checks

- Task-scoped scope store verified at unit level (`scope_task_isolation_test.go`).
- Worker wiring (per-task seed/clear, containment) reviewed in `PHASE8_4_2_WORKER_ENFORCEMENT.md`.
- Bash guard, WebFetch/WebSearch disable, MCP drop: `PHASE8_4_2_BYPASS_CONTAINMENT.md`.
