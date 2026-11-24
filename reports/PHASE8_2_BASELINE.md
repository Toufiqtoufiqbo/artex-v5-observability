# Phase VIII.2 — PHASE8_2_BASELINE

## P0 root-cause: `company fails open`

**Exact code reference (pre-fix):** `agent/scopecheck.go:86-87`
```go
case "company":
    return nil
```
`CheckScope` iterates the task's scope entries. For `Kind=="company"` it returned `nil` (authorized) unconditionally — without checking that the request host belongs to that company.

1. Function: `CheckScope(rawURL)` in `agent/scopecheck.go`.
2. On company-scope evaluation: always authorized, no validation.
3. Missing/malformed company info: still authorized (`CompanyID` never read).
4. Fallback: yes — a company rule silently broadened the scope to every host; other rules could never override it (first match wins).
5. Bypass paths: any tool using `doHTTP` (HTTPRequest, Probe, Crawl, Login, Session.auth_check, IdorDiff, Fuzz, Retest) when task scope contains a `company` entry.

**Reproduction (pre-fix):** `SetTaskScope([]db.TaskScope{{Kind:"company", CompanyID:&cid}})` → `CheckScope("http://anything.example/")` → nil (authorized).

**Existing tests:** `TestCheckScope_RootDomain`, `TestCheckScope_IP`, `TestCheckScope_NoScopeAllows` — none covered company.

**Limitations of this audit:** only the company path was traced in depth; redirect/DNS/concurrent paths were not independently reproduced in this mission (budget).
