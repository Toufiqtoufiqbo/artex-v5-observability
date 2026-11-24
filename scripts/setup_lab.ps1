# scripts/setup_lab.ps1 — disposable ARTEX lab (IX.2.4 release-readiness)
# Creates/tears down a lab DB + roles; never touches other databases.
param(
  [string]$DbName = 'artex_lab',
  [string]$MigrationRole = 'artex_lab_mig',
  [string]$RuntimeRole = 'artex_lab_rt',
  [int]$PgPort = 55432,
  [string]$PgUser = 'postgres',
  [string]$PgHost = '127.0.0.1',
  [switch]$Teardown
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$pgBin = Join-Path (Split-Path -Parent $root) 'pg\postgresql-18.7.0-x86_64-pc-windows-msvc\bin'
if (-not (Test-Path (Join-Path $pgBin 'psql.exe'))) { $pgBin = 'C:\Program Files\PostgreSQL\18\bin' }

function Invoke-Psql([string]$db, [string]$sql) {
  & (Join-Path $pgBin 'psql.exe') -h $PgHost -p $PgPort -U $PgUser -d $db -v ON_ERROR_STOP=1 -c $sql
  if ($LASTEXITCODE -ne 0) { throw "psql failed: $sql" }
}

if ($Teardown) {
  Invoke-Psql 'postgres' "DROP DATABASE IF EXISTS $DbName"
  Invoke-Psql 'postgres' "DROP ROLE IF EXISTS $MigrationRole"
  Invoke-Psql 'postgres' "DROP ROLE IF EXISTS $RuntimeRole"
  Write-Host "Lab removed: $DbName / $MigrationRole / $RuntimeRole"
  exit 0
}

# Create roles (idempotent via DO block) and a fresh DB.
foreach ($role in @($MigrationRole, $RuntimeRole)) {
  $exists = & (Join-Path $pgBin 'psql.exe') -h $PgHost -p $PgPort -U $PgUser -d postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='$role'"
  if ($LASTEXITCODE -ne 0) { throw "role lookup failed" }
  if ($exists -ne '1') {
    Invoke-Psql 'postgres' "CREATE ROLE $role LOGIN"
  }
}
Invoke-Psql 'postgres' "DROP DATABASE IF EXISTS $DbName"
Invoke-Psql 'postgres' "CREATE DATABASE $DbName OWNER $MigrationRole"

# Apply schema with the migration role, then grant DML to runtime role.
$schema = Join-Path $root 'db\schema.sql'
& (Join-Path $pgBin 'psql.exe') -h $PgHost -p $PgPort -U $MigrationRole -d $DbName -v ON_ERROR_STOP=1 -q -f $schema
if ($LASTEXITCODE -ne 0) { throw "schema apply failed" }
foreach ($t in @('tasks','explorations','exploration_nodes','exploration_edges','activity','findings','assets','companies')) {
  # llm_usage is created lazily by the app (db/llm_usage.go), so skip it here.
  $exists = & (Join-Path $pgBin 'psql.exe') -h $PgHost -p $PgPort -U $PgUser -d $DbName -tAc "SELECT to_regclass('public.$t') IS NOT NULL"
  if ($exists -eq 't') {
    Invoke-Psql $DbName "GRANT SELECT, INSERT, UPDATE, DELETE ON $t TO $RuntimeRole"
  }
}

	# The runtime role needs sequence USAGE for SERIAL/BIGSERIAL ids.
	Invoke-Psql $DbName "GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO $RuntimeRole"

Write-Host "Lab ready: $DbName (migration=$MigrationRole, runtime=$RuntimeRole)"
Write-Host "DSN: postgres://$RuntimeRole@$($PgHost):$($PgPort)/$($DbName)?sslmode=disable"
