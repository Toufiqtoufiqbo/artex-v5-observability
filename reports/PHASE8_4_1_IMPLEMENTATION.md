# PHASE8_4_1 — IMPLEMENTATION (Phase C)

Minimum-change implementation. The smallest set of source files is in `agent/`; `db/`, `server/`, `cmd/` are untouched.

## 1. Files changed

```
CHANGED (this phase)
├── agent/scopecheck.go                 (modified by this phase)
├── agent/scopecheck_test.go            (modified by this phase)
├── agent/tools_probe_test.go           (test fix: seed scope)
├── agent/tools_login_test.go           (test fix: seed scope)
├── agent/finding_gate_test.go          (test fix: seed scope)
├── agent/tools_diff_test.go            (test fix: seed scope)
├── agent/tools_attackpath_test.go      (test fix: seed scope)
└── agent/scope_failclosed_test.go      (NEW)

UNCHANGED pre-existing state (preserved):
├── agent/tools.go                      (pre-existing dirty, untouched)
├── agent/worker.go                     (pre-existing dirty, untouched)
└── everything else (v5 additions listed in backups\phase8_4_1\git-status-short.txt)
```

Pre-phase hashes recorded in `backups\phase8_4_1\phase-change-hashes.txt`; post-phase hashes in the same file (appended by Phase F review).

## 2. `agent/scopecheck.go` — new policy

### Sentinels

```go
var (
	ErrScopeMissing   = errors.New("scope authorization missing")
	ErrScopeEmpty     = errors.New("scope authorization empty")
	ErrScopeMalformed = errors.New("scope authorization malformed")
	ErrScopeDenied    = errors.New("scope authorization denied")
)
```

All error messages contain the word "scope" (existing log-grep consumers keep working). `errors.Is` callers can distinguish categories.

### `CheckScope` (rewritten)

```go
func CheckScope(rawURL string) error {
	entries, seeded := scopeState()
	if !seeded {
		return fmt.Errorf("%w: no target scope is configured; network assessment is denied", ErrScopeMissing)
	}
	if len(entries) == 0 {
		return fmt.Errorf("%w: target scope is empty; network assessment is denied", ErrScopeEmpty)
	}
	if err := validateScopeEntries(entries); err != nil {
		return err // ErrScopeMalformed — the configuration is rejected
	}
	// ... host extraction + match as before, but:
	case "ip":
		if hip != nil {
			if eip := scopeEntryIP(e); eip != nil && eip.Equal(hip) {
				return nil
			}
		}
	case "cidr":
		if hip != nil {
			n := strings.TrimSpace(e.Net)
			if n != "" {
				if _, ipnet, err := net.ParseCIDR(n); err == nil && ipnet.Contains(hip) {
					return nil
				}
			}
		}
	// out of scope -> ErrScopeDenied
}
```

### `SetTaskScope` semantics change

```go
func SetTaskScope(entries []db.TaskScope) {
	scopeMu.Lock(); defer scopeMu.Unlock()
	scopeEntries = append([]db.TaskScope{}, entries...)
	scopeSeeded = true        // empty list now = explicitly empty scope
}
```

Rationale: `SetTaskScope` is called by `Worker.execute` only when `len(scopes) > 0`, otherwise `ClearTaskScope()` — so worker semantics are unchanged; the change makes the global store *strictly safer* (no stale prior-task scope can be inherited as authorization).

### New validation (`validateScopeEntry`)

- `company`: requires `CompanyID` non-nil, > 0. (Company entries never match a host at this layer — they remain non-authorizing.)
- `root_domain`/`subdomain`: non-empty `Domain`.
- `ip`: `scopeEntryIP(e) != nil` (Value or Net, bare IP or bracketed IPv6).
- `cidr`: `Net` parses via `net.ParseCIDR`.
- `icp`/`keyword`: non-empty `Value` (known non-host kinds; a scope of only these denies).
- anything else: `ErrScopeMalformed` (unsupported kind).

Any invalid entry rejects the whole configuration: `ErrScopeMalformed` — never "discard and accept the rest".

### New helpers

- `scopeEntryIP(e db.TaskScope) net.IP` — resolves the authorized IP from `Value` (bare/bracketed) or `Net` (bare IP or CIDR). This fixes the demonstrated defect that real auto-scope entries (`Kind:"ip", Net:"127.0.0.1/32"`) did not authorize their own host.
- `scopeEntryLabel(e)` — small label helper for error text.
- `validateScopeEntries(entries)`.

Removed: `hostIP` (unused after the ip-branch rewrite).

## 3. Requirement → implementation mapping

| Requirement | Implementation |
|---|---|
| Missing scope → DENY | `!seeded` → `ErrScopeMissing` (`CheckScope:97-99`) |
| Nil scope → DENY | unseeded is nil store; also `SetTaskScope(nil)` now means explicit-empty → `ErrScopeEmpty` |
| Empty scope → DENY | `len(entries)==0` → `ErrScopeEmpty` |
| Unseeded → DENY | as above |
| Malformed → DENY | `validateScopeEntries` → `ErrScopeMalformed` |
| Unsupported type → DENY | `validateScopeEntry` default branch |
| Valid scope → match | existing match semantics retained (company still never matches here; root_domain/subdomain/ip/cidr as documented) |
| Out-of-scope destination → DENY | falls through to `ErrScopeDenied` |
| No silent discard of invalid entries | validation is all-or-nothing |
| Validation before transport | `doHTTP` calls `scopeGuard` before `client.Do` (unchanged position) |
| Existing valid scopes keep working | `TestCheckScope_RootDomain`, `TestCheckScope_IP` (Value form) pass |
| Redirect validation active | unchanged (`httpclient.go:214-229`) |
| Cross-origin credential stripping | unchanged |
| Dial-level accounting active | unchanged (`dialFn`) |
| Distinguish denial kinds | `ErrScopeMissing/Empty/Malformed/Denied` sentinels |
| No sensitive data in errors | messages contain only kinds/hosts/scopes |
| Failed auth not converted to success | `doHTTP` returns the error; tools propagate it |
| Provider egress untouched | `agent/provider.go` uses its own `http.Client`; does not call `scopeGuard` — separate authorization domain |
| Health/auth endpoints unaffected | they do not go through `doHTTP` |
| Detection/prompts/schemas unchanged | none edited |

## 4. Test scaffolding

- `seedLoopbackScope(t, rawURL)` (new, `agent/scopecheck_test.go`): seeds an explicit loopback IP scope for tests that exercise real network paths; asserts the destination is a loopback IP literal; cleanup calls `ClearTaskScope`.
- Updated tests that previously relied on default-allow to seed loopback scope:
  - `agent/tools_probe_test.go`: TestProbeSQLiBoolean, TestProbeSQLiBooleanNegative, TestProbeXSSReflect, TestProbeSSTI, TestProbeTraversal, TestSessionCookieJar, TestCrawl
  - `agent/tools_diff_test.go`: TestIdorDiff, TestIdorDiffSame, TestFuzz
  - `agent/tools_login_test.go`: TestLoginCookieJarFlow
  - `agent/finding_gate_test.go`: TestHTTPRequestTool
  - `agent/tools_attackpath_test.go`: TestRetest

This is test-environment alignment with the new policy, not weakening of assertions.

## 5. Hashes

- Pre-phase: `backups\phase8_4_1\phase-change-hashes.txt` (captured before any edit)
- Post-phase: appended by Phase F (final diff review)
