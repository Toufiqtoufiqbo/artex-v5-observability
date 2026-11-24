# Configuration & Privacy Notes

## Environment variables (verified paths in this repo)
| Variable | Purpose | Required |
|---|---|---|
| `ARTEX_PG_DSN` | PostgreSQL DSN for app runtime (restricted role) | yes for server/benchmarks |
| `ARTEX_DB_SKIP_MIGRATION` | `1` = skip auto schema application (use when migration already ran) | optional |
| `ARTEX_TELEMETRY_DIR` | Enables execution telemetry and sets JSONL/events dir | optional |

## Least privilege
- Migration role: applies `db/schema.sql` (DDL), used only at setup.
- Runtime role: DML only. Test suites that attempt DDL will fail/skip under it — this is expected (documented environmental set in IX.2.2/IX.2.3 reports).

## Telemetry privacy (IX.2.3–IX.2.3.3)
- Events store metadata only: ids, UTC timestamps, durations, outcomes, bounded error class, token counts.
- No prompts, HTTP authorization headers, cookies, DSNs, or private evidence bodies.
- `usage_metadata.available=false` means "provider did not report usage" — **never** 0 tokens.
- Labels are bounded enums (`read|write|shell|web|other`; `validation_error|authorization_rejection|...`). No endpoint strings, task ids, or prompt fragments as metric labels.
- Append-only JSONL with at-most-once rotation; malformed lines are skipped on read; write failures are counted, never fatal to execution.

## Diagnostics API auth
Routes `/api/diagnostics/*` are registered on the same authenticated mux as the rest of the API (`server/server.go`). They expose aggregates/metadata only.

## Known gaps (do not misread metrics)
- IX.2.1.1 finding-name fallback is now FIXED (see `GLOBAL_REAUDIT_FIXES_RESULTS.md`), but older benchmark artifacts reflect pre-fix behavior.
- `RACE DETECTOR UNKNOWN — ENVIRONMENTAL VERIFICATION DEBT` remains.
- Empty scope remains default-allow — configure explicit scope for every task.
