# PHASE8_4_3 — RUNTIME VALIDATION (Phase H)

## 1. Identity

| Field | Value |
|---|---|
| Binary | `D:\pc\Disktop\New folder (2)\artex-v5-843.exe` (separate file; `artex-v5-842.exe` untouched) |
| SHA-256 | `27ACF2013DD9CEA99A42174CBB1565B9D2B92D4A842FF1EBE360826D3A38AD67` |
| Build | `go build -o artex-v5-843.exe ./cmd/artex`, exit 0 |
| Source commit | `e6ec56999175509551a1e43a3ee3b053d0941bd9` (detached HEAD) |
| Go version | `go1.26.11 windows/amd64` |
| Database | `artex_v43` (fresh) |
| Data dir | `D:\pc\Disktop\New folder (2)\artex-v43-data` |
| Port / proxy | 127.0.0.1:8801 / 127.0.0.1:8803 (traffic proxy) |
| Log | `D:\pc\Disktop\New folder (2)\v5-843.log` |

## 2. Startup evidence

```
PID (listening): 11480
GET /api/health → 200 {"ok":true,"service":"artex","version":"dev"}
POST /api/auth/init (<admin password, redacted>) → 200
POST /api/llm → 200; GET /api/llm → 200 (provider openai, model nvidia/nemotron-3.5-lightning-30b-a3b)
Log: "ARTEX dev backend listening on 127.0.0.1:8801 (data=..., workers=3)"
     "[traffic] recording proxy on 127.0.0.1:8803 ..."
```

Existing services (8787/8888/8799) **not terminated or replaced** (verified up after validation).

## 3. Runtime checks (Phase H list)

1. ✅ Intended executable running: PID 11480, `artex-v5-843.exe`.
2. ✅ Listener on loopback: 8801 and proxy 8803 UP.
3. ✅ Health: 200.
4. ✅ Auth: init + login OK.
5. ✅ DB: `artex_v43` fresh; health+login imply connectivity (performed).
6. ✅ Target egress policy: unit-verified via traffic proxy tests + runtime proxy port UP (`PHASE8_4_3_TRAFFIC_PROXY.md`).
7. ✅ Provider egress policy: unit-verified; provider client untouched (norma).
8. ✅ Proxy restrictions: traffic proxy allowlist tests PASS (`traffic/egress_test.go`).
9. ✅ Subprocess network: guarded by Bash guard; containment units PASS (`PHASE8_4_3_SUBPROCESS_CONTAINMENT.md`).
10. ✅ Non-network endpoints: health/auth OK.

## 4. Do-not-touch verification

- No real NVIDIA inference during this phase.
- No external system contacted.
- DB `artex_v43` is new; old DBs (`artex`, `artex_base`, `artex_pilot`, `artex_v42`) untouched.
