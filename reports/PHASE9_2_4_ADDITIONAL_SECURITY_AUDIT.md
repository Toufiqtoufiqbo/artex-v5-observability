# SECURITY AUDIT — ADDITIONAL VULNERABILITY SEARCH & FIX
## Results Report

**Generated:** 2026-11-24
**HEAD:** `e6ec56999175509551a1e43a3ee3b053d0941bd9`
**Scope:** read-only security review of current tree + one bounded fix. No external targets, no new offensive capability.

---

## 1. Findings

| ID | Finding | Status | Evidence |
|---|---|---|---|
| VULN-1 | Telemetry event store unbounded: `Config.MaxFileSize` declared but never enforced; no rotation, no file cap → unbounded disk growth under sustained runs | **FIXED AND TEST VERIFIED** | `telemetry/store.go`: rotation at MaxFileSize (default 50 MB), max 3 rotated files, 0600 perms; `telemetry/rotation_test.go` |
| SEC-1 | "empty-scope default-allow" from Atlas | **ALREADY FIXED (stale note)** | `agent/scopecheck.go:26-31` fail-closed + `scopecheck_test.go` |
| SEC-2 | Proxy policy / egress allowlist | **PRESENT** (source verified) | `traffic/egress.go`, `egress_dial.go`, `egress_allowlist_test.go`, `agent/proxyenv_test.go` |
| SEC-3 | Secrets in benchmark artifacts or logs | **NOT FOUND** | grep across all artifacts + Go sources for `postgres://`, `Bearer`, `api_key`, `Authorization:` → 0 hits |
| SEC-4 | Diagnostics API auth | **JWT-protected**; no per-task ownership (single-user platform model) | `server/server.go:983 api := cors(s.requireAuth(mux))` |
| SEC-5 | CORS `Access-Control-Allow-Origin: *` | **LOW RISK** (JWT in header, not cookies) — noted, not changed | `server/server.go:3933` |
| SEC-6 | `RACE DETECTOR UNKNOWN — ENVIRONMENTAL VERIFICATION DEBT` | **UNRESOLVED** | no supported race detector in this environment |

## 2. Fix details — VULN-1

- `telemetry/store.go`:
  - `defaultMaxFileSize = 50 MB`, `maxRotatedFiles = 3`.
  - `Recorder.rotateIfNeeded()` before each append; rename active file to `events-<UTC-timestamp>.jsonl`, prune oldest beyond cap, reopen new file.
  - `ReadEvents()` now reads active + rotated files (sorted chronologically).
  - Event files created with `0600` permissions (privacy).
- `telemetry/rotation_test.go::TestRecorderRotatesAndBoundsDisk` — passes (12 events, tiny ceiling, bounded files, all events readable).

## 3. Validation

| Command | Exit |
|---|---|
| `go build ./...` | 0 |
| `go vet ./telemetry` | 0 |
| `go test ./telemetry -count=1` (14 tests) | 0 (ok) |
| `git diff --check` | 0 |

## 4. Verdict (exact)

**VERDICT: PASS — ADDITIONAL SECURITY REVIEW COMPLETED; ONE RESOURCE-GROWTH DEFECT FIXED AND VERIFIED**
