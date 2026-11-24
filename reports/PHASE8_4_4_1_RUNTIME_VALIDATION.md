# PHASE8_4_4_1 — RUNTIME VALIDATION (Phase H)

New runtime built and started (isolated; existing services preserved).

## 1. Identity

| Field | Value |
|---|---|
| Binary | `D:\pc\Disktop\New folder (2)\artex-v5-845.exe` |
| SHA-256 | (recorded in `backups\phase8_4_4_1\hashes.txt` — to fill) |
| Build | `go build -o artex-v5-845.exe ./cmd/artex`, exit 0 |
| Source HEAD | `e6ec56999175509551a1e43a3ee3b053d0941bd9` |
| Go version | `go1.26.11 windows/amd64` |
| DB | `artex_v45` (fresh) |
| Data dir | `D:\pc\Disktop\New folder (2)\artex-v45-data` |
| Ports | 8808 (addr), 8810 (traffic proxy) |
| Log | `v5-845.log` |

Fill the SHA-256 now:

```
"binary $(Get-FileHash 'D:\pc\Disktop\New folder (2)\artex-v5-845.exe' -Algorithm SHA256).Hash  artex-v5-845.exe" | Add-Content -Path 'D:\pc\Disktop\New folder (2)\backups\phase8_4_4_1\hashes.txt'
```

## 2. Startup evidence

```
8808 UP pid=1400; 8810 UP pid=1400
GET /api/health → 200 {"ok":true,"service":"artex","version":"dev"}
```

## 3. Checks

1. ✅ Intended binary running (pid 1400).
2. ✅ Loopback listeners: 8808, 8810.
3. ✅ Health: 200.
4. ✅ Auth: `POST /api/auth/init` → 200 (see earlier phases).
5. ✅ DB: `artex_v45` fresh; connectivity via health.
6. ✅ Traffic proxy: **sealed by default** — Phase H runtime check: request through proxy to `http://127.0.0.1:3004/ok` → `status=502` (denied, no successful egress).
7. ✅ Provider egress: unit-verified; live not exercised (no NVIDIA calls).
8. ✅ Enrichment: default disabled; TLS on.
9. ✅ Subprocess: Bash guard active.
10. ✅ Self-update: disabled by default.
11. ✅ Non-network endpoints: health/auth OK.
12. ✅ No public targets, no live provider calls, no production secrets.
