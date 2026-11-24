# PHASE8_3_1 — BASELINE BUILD

**Source:** `artex-global-main.zip` (extracted to `%TEMP%\opencode\artex-base\artex-global-main`).
- SHA-256: `93AFBEC089A9D8AB00AE1DA4918DFCAF3A99AF636C1303DAE514F4EBE7CD11E2`, 8,944,269 bytes.
- Identity: NOT fully proven — no git metadata in archive; reference commit `133f3cf…` unverifiable locally; relation between archive and `v0.3.15` unproven.

**Build (executed):**
```
cd <baseline>
go build ./...
exit=0            # backend builds with go1.26.11
```
`go.mod` requires `go 1.26.3`, `norma v0.4.4` (modified uses v0.4.3 — version skew documented).

**Tests (executed, unmodified):**
```
go test ./agent/ -count=1
ok  github.com/Autumn-27/artex/agent  0.078s   exit=0
```

**Not executed:** frontend build, runtime launch, DB-backed/API tests.

Isolated copies: baseline extracted separately; modified tree untouched (`modified_v5` = `artex-original`). Build noise (`server/webui/dist/_next/**`) excluded from source comparison only — not deleted.
