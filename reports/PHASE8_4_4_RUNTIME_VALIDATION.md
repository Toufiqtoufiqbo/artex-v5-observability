# PHASE8_4_4 â€” RUNTIME VALIDATION (Phase H)

Isolated runtime of the VIII.4.4 binary.

## 1. Identity

| Field | Value |
|---|---|
| Binary | `<WORKSPACE>\artex-v5-844.exe` (new; prior 843 untouched) |
| SHA-256 | `E230E787233DA11ECC4E7AA91BAB44C3C276B877263EA99DECFB951FAEFBA643` |
| Build | `go build -o artex-v5-844.exe ./cmd/artex`, exit 0 |
| Source commit | `e6ec56999175509551a1e43a3ee3b053d0941bd9` |
| Go version | `go1.26.11 windows/amd64` |
| Database | `artex_v44` (fresh) |
| Data dir | `<WORKSPACE>\artex-v44-data` |
| Port / proxy | 127.0.0.1:8805 / 127.0.0.1:8807 |
| Log | `<WORKSPACE>\v5-844.log` |

## 2. Startup evidence

```
PID: 1744
GET /api/health â†’ 200 {"ok":true,"service":"artex","version":"dev"}
POST /api/auth/init â†’ 200; POST /api/llm â†’ 200 (same NVIDIA config as VIII.4.3)
Log: "[traffic] recording proxy on 127.0.0.1:8807 ..."
```

## 3. Checks

1. âœ… Intended binary running (pid 1744, `artex-v5-844.exe`).
2. âœ… Loopback listeners: 8805, 8807.
3. âœ… Health: 200.
4. âœ… Auth init + login.
5. âœ… DB: `artex_v44` fresh; connectivity via health+login.
6. âœ… Target egress: unit-verified (`TestProviderEgress*`, `TestTrafficProxyEgress*`).
7. âœ… Provider egress: transport enforced (see `PROVIDER_EGRESS.md`).
8. âœ… Proxy restrictions: allowlist unit-verified + listener UP.
9. âœ… Enrichment TLS/egress: unit-verified (`TestBuildClientTLSVerification`, `TestEnrichJobEgressPolicy`); default disabled.
10. âœ… Subprocess: Bash guard active.
11. âœ… Non-network endpoints: health/auth OK.
12. âœ… Self-update: disabled by default.

## 4. No live NVIDIA, no public targets, no production secrets.
