# ARTEX v5 — Quickstart

**Status:** functionally complete and verified (IX.2.4: 7/7 detection + 8/8 planning). This guide covers local, authorized use only.

---

## 1. Prerequisites
- Go toolchain (repo uses Go 1.26+) on PATH.
- PostgreSQL 18.x running locally (default lab port used here: `55432`).
- A disposable database and two roles (migration for DDL, runtime for app DML).

## 2. Build
```powershell
go build ./...
go vet ./...
```

## 3. Create a disposable lab (PowerShell)
Run `scripts/setup_lab.ps1` with your own names:
```powershell
powershell -ExecutionPolicy Bypass -File scripts/setup_lab.ps1 `
  -DbName artex_lab -MigrationRole artex_lab_mig -RuntimeRole artex_lab_rt `
  -PgPort 55432 -PgUser postgres
```
The script creates the DB + roles, applies `db/schema.sql` with the migration role, then grants DML to the runtime role. It never touches existing databases. To tear down: `scripts/setup_lab.ps1 -Teardown ...` (same names).

## 4. Run the backend
```powershell
$env:ARTEX_PG_DSN = 'postgres://artex_lab_rt@localhost:55432/artex_lab?sslmode=disable'
$env:ARTEX_TELEMETRY_DIR = 'data/telemetry'     # optional; enables execution telemetry
go run ./cmd/artex -addr :8787 -data ./data
```

## 5. Run the benchmark suites
```powershell
$env:ARTEX_PG_DSN = 'postgres://artex_lab_rt@localhost:55432/artex_lab?sslmode=disable'
go run ./cmd/artex benchmark list
go run ./cmd/artex benchmark run -suite artex-v5-offline-baseline -mode offline -out benchmark-artifacts/my-run-1 -seed 1
go run ./cmd/artex benchmark run -suite artex-v5-planning-v922 -mode offline -out benchmark-artifacts/my-run-2 -seed 1
```
Expected (IX.2.4 proof): offline 7/7 TP=4 FP=0; planning 8/8, inflation=0.

## 6. Diagnostics
```powershell
go run ./cmd/artex diagnostics summary
go run ./cmd/artex diagnostics task <task-id>
go run ./cmd/artex diagnostics metrics
```
HTTP (with the existing auth) additionally exposes:
`GET /api/diagnostics/summary|run|task|timeline|trend|compare|tools`.

## 7. Safety
- Use only against systems you own or have written authorization to test.
- Scope is enforced by `agent/scopecheck.go` + dial accounting; empty scope is default-allow (known gap — set explicit scope).
- No API keys, prompts, cookies or DSNs are written to telemetry events; see `docs/CONFIG_AND_PRIVACY.md`.

## 8. Where the proof lives
- `reports/PHASE9_2_4_STABILITY_AND_PROOF_RESULTS.md` — benchmark artifacts and run IDs.
- `reports/GLOBAL_PROJECT_REAUDIT_V2.md` — full source-level phase audit.
