package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"

	"github.com/Autumn-27/artex/db"
)

// VIII.4.1 — fail-closed target-scope authorization.
//
// Before this change the authority was effectively "authorized by default":
// CheckScope returned nil when nothing was seeded, and invalid entries were
// silently skipped. Now every authorization decision is one of:
//   - a valid scope that matches the destination,
//   - a deny with one of the sentinel categories below.
//
// The sentinel categories let callers and tests distinguish why a request was
// refused; messages intentionally contain the word "scope" for log grep.
var (
	ErrScopeMissing   = errors.New("scope authorization missing")
	ErrScopeEmpty     = errors.New("scope authorization empty")
	ErrScopeMalformed = errors.New("scope authorization malformed")
	ErrScopeDenied    = errors.New("scope authorization denied")
)

// ---------------------------------------------------------------------------
// Scope enforcement (Phase 7 — Operational safety, hard floor)
//
// The scope is declared by the task (planner's add_task_scope / set_constraints
// or the task goal's "scope strictly ..."). The worker seeds it via
// SetTaskScope, and every network primitive (HTTPRequest, Probe, IdorDiff,
// Fuzz, Crawl, Login) consults it in doHTTP BEFORE any request is sent.
//
// VIII.4.1 policy — fail closed. Every outcome is an explicit decision:
//   - Valid scope entry matching the destination → allowed.
//   - Unseeded scope → ErrScopeMissing (deny).
//   - Explicitly empty scope → ErrScopeEmpty (deny).
//   - Invalid/malformed scope entry or unsupported kind → ErrScopeMalformed
//     (deny the whole configuration; invalid entries are never silently
//     discarded while other entries authorize).
//   - Valid scope but destination not matched → ErrScopeDenied (deny).
//
// Matching rules for valid entries:
//   - company    → never matches by itself here (no asset resolution at this
//                  layer); it remains a non-authorizing scope row.
//   - root_domain → host == domain or host ends with "."+domain
//   - subdomain  → host == domain
//   - ip         → destination host IP equals the entry IP (from Value, or
//                  from Net in bare-IP / CIDR form)
//   - cidr       → destination host IP is inside the entry net
//
// Note (honesty): this is a best-effort host match, not a policy engine. If
// the scope says root_domain "example.com" then sub.example.com IS in scope.
// ---------------------------------------------------------------------------

var (
	scopeMu sync.RWMutex
	// VIII.4.2 — scope storage is per-task, keyed by the task registry id.
	// The legacy key "" keeps the pre-VIII.4.2 global store semantics for
	// callers that do not have a task (tests, non-worker paths).
	scopes = map[string]taskScopeState{}
)

// taskScopeState is the immutable authorization snapshot for one scope key.
type taskScopeState struct {
	entries []db.TaskScope
	seeded  bool
}

// scopeKeyForTask maps a task registry id to its scope key; non-positive ids
// map to the legacy global key "".
func scopeKeyForTask(taskID int64) string {
	if taskID <= 0 {
		return ""
	}
	return strconv.FormatInt(taskID, 10)
}

// scopeKeyFromContext resolves the scope key of the executing task from the
// Worker's RunInfo context value; callers without a task get the legacy key.
func scopeKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	return scopeKeyForTask(RunInfoFrom(ctx).TaskID)
}

func setScope(key string, entries []db.TaskScope) {
	scopeMu.Lock()
	defer scopeMu.Unlock()
	scopes[key] = taskScopeState{entries: append([]db.TaskScope{}, entries...), seeded: true}
}

func clearScope(key string) {
	scopeMu.Lock()
	defer scopeMu.Unlock()
	delete(scopes, key)
}

func getScope(key string) (taskScopeState, bool) {
	scopeMu.RLock()
	defer scopeMu.RUnlock()
	s, ok := scopes[key]
	return s, ok
}

// SetTaskScope seeds the process-wide (legacy, key "") scope store.
func SetTaskScope(entries []db.TaskScope) { setScope("", entries) }

// SetTaskScopeForTask seeds the authorization context of one task (VIII.4.2).
// An empty entry list marks the task scope as EXPLICITLY empty
// (ErrScopeEmpty on any request). Callers that pass a non-positive taskID get
// the legacy global store.
func SetTaskScopeForTask(taskID int64, entries []db.TaskScope) {
	setScope(scopeKeyForTask(taskID), entries)
}

// ClearTaskScope drops the legacy (key "") scope.
func ClearTaskScope() { clearScope("") }

// ClearTaskScopeForTask drops the scope of one task (VIII.4.2), so no state
// can leak into a later task reusing this Worker.
func ClearTaskScopeForTask(taskID int64) { clearScope(scopeKeyForTask(taskID)) }

// SeedTaskScopeFromStore loads a task's declared scope into the in-memory
// per-task store. Every agent that may send network traffic for the task must
// call this before running: the store is keyed per task and is no longer
// dropped when an individual worker exits, otherwise a worker finishing would
// revoke the planner's authorization mid-task ("no target scope is configured").
func SeedTaskScopeFromStore(as *db.AssetStore, taskID int64) {
	if as == nil {
		ClearTaskScopeForTask(taskID)
		return
	}
	scopes, err := as.ListTaskScopeWithSources(taskID)
	if err != nil || len(scopes) == 0 {
		ClearTaskScopeForTask(taskID)
		return
	}
	SetTaskScopeForTask(taskID, scopes)
}

// CheckScope validates the destination of rawURL against the legacy global
// store (key ""). VIII.4.2 production paths go through CheckScopeForTask.
func CheckScope(rawURL string) error { return CheckScopeForKey("", rawURL) }

// CheckScopeForKey is VIII.4.2's per-task scope check. See the file header for
// the fail-closed policy; the returned error is a non-nil sentinel.
func CheckScopeForKey(key string, rawURL string) error {
	st, ok := getScope(key)
	if !ok {
		return fmt.Errorf("%w: no target scope is configured for this task; network assessment is denied", ErrScopeMissing)
	}
	entries, seeded := st.entries, st.seeded
	if !seeded {
		return fmt.Errorf("%w: target scope is unseeded; network assessment is denied", ErrScopeMissing)
	}
	if len(entries) == 0 {
		return fmt.Errorf("%w: target scope is empty; network assessment is denied", ErrScopeEmpty)
	}
	if err := validateScopeEntries(entries); err != nil {
		return err
	}
	u := strings.TrimSpace(rawURL)
	host := hostOf(u)
	if host == "" {
		return fmt.Errorf("%w: could not parse host from %q", ErrScopeDenied, rawURL)
	}
	lowHost := strings.ToLower(host)
	hip := net.ParseIP(lowHost)
	for _, e := range entries {
		switch kind := strings.ToLower(strings.TrimSpace(e.Kind)); kind {
		case "company":
			// Non-authorizing row (see header). Rejected entries are
			// rejected again by validateScopeEntries, so this is purely
			// informational for a well-formed configuration.
			continue
		case "root_domain":
			if d := strings.ToLower(strings.TrimSpace(e.Domain)); d != "" &&
				(lowHost == d || strings.HasSuffix(lowHost, "."+d)) {
				return nil
			}
		case "subdomain":
			if d := strings.ToLower(strings.TrimSpace(e.Domain)); d != "" && lowHost == d {
				return nil
			}
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
		}
	}
	return fmt.Errorf("%w: host %q is outside the declared scope %s", ErrScopeDenied, host, scopeSummary(entries))
}

// validateScopeEntries rejects a malformed or internally inconsistent
// configuration instead of silently reducing or expanding its permissions.
func validateScopeEntries(entries []db.TaskScope) error {
	for _, e := range entries {
		if err := validateScopeEntry(e); err != nil {
			return err
		}
	}
	return nil
}

func validateScopeEntry(e db.TaskScope) error {
	kind := strings.ToLower(strings.TrimSpace(e.Kind))
	switch kind {
	case "company":
		if e.CompanyID == nil || *e.CompanyID <= 0 {
			return fmt.Errorf("%w: company entry without a valid company id", ErrScopeMalformed)
		}
	case "root_domain", "subdomain":
		if strings.TrimSpace(e.Domain) == "" {
			return fmt.Errorf("%w: %s entry without a domain", ErrScopeMalformed, kind)
		}
	case "ip":
		if scopeEntryIP(e) == nil {
			return fmt.Errorf("%w: ip entry %q is not a valid IP", ErrScopeMalformed, scopeEntryLabel(e))
		}
	case "cidr":
		n := strings.TrimSpace(e.Net)
		if n == "" {
			return fmt.Errorf("%w: cidr entry without a network", ErrScopeMalformed)
		}
		if _, _, err := net.ParseCIDR(n); err != nil {
			return fmt.Errorf("%w: cidr entry %q is invalid", ErrScopeMalformed, n)
		}
	case "icp", "keyword":
		if strings.TrimSpace(e.Value) == "" {
			return fmt.Errorf("%w: %s entry without a value", ErrScopeMalformed, kind)
		}
	default:
		return fmt.Errorf("%w: unsupported scope kind %q", ErrScopeMalformed, e.Kind)
	}
	return nil
}

// scopeEntryIP resolves the IP a scope entry authorizes, from Value (bare
// IP or bracketed IPv6) or from Net (bare IP or CIDR). nil when the entry
// carries no valid IP.
func scopeEntryIP(e db.TaskScope) net.IP {
	if v := strings.TrimSpace(e.Value); v != "" {
		if ip := net.ParseIP(v); ip != nil {
			return ip
		}
		if len(v) > 1 && v[0] == '[' && v[len(v)-1] == ']' {
			if ip := net.ParseIP(v[1 : len(v)-1]); ip != nil {
				return ip
			}
		}
	}
	n := strings.TrimSpace(e.Net)
	if n == "" {
		return nil
	}
	if strings.Contains(n, "/") {
		if ip, _, err := net.ParseCIDR(n); err == nil {
			return ip
		}
		return nil
	}
	return net.ParseIP(n)
}

func scopeEntryLabel(e db.TaskScope) string {
	if v := strings.TrimSpace(e.Value); v != "" {
		return v
	}
	if n := strings.TrimSpace(e.Net); n != "" {
		return n
	}
	return e.Domain
}

func hostOf(raw string) string {
	s := strings.TrimSpace(raw)
	// Strip scheme if present.
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// Cut at first '/'.
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	// Drop userinfo.
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	// Strip port — net.SplitHostPort handles bracketed IPv6; avoid truncating
	// a bare IPv6 literal (multiple colons) with a naive LastIndex.
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	} else if strings.Count(s, ":") == 1 {
		if i := strings.LastIndex(s, ":"); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(strings.Trim(s, "[]"))
}

// addressAuthorized reports whether a concrete address is authorized by an
// explicit `ip` or `cidr` scope entry (VIII.4.5). Hostname-based entries
// (root_domain/subdomain/company/icp/keyword) do not authorize addresses —
// they authorize hostnames, checked by CheckScope.
func addressAuthorized(key string, ip net.IP, host string) bool {
	if ip == nil {
		return false
	}
	st, ok := getScope(key)
	if !ok {
		return false
	}
	if a, ok := netip.AddrFromSlice(ip); ok {
		ip = net.IP(a.Unmap().AsSlice()) // IPv4-mapped IPv6 → IPv4
	}
	low := strings.ToLower(strings.TrimSpace(host))
	for _, e := range st.entries {
		switch kind := strings.ToLower(strings.TrimSpace(e.Kind)); kind {
		case "ip":
			if eip := scopeEntryIP(e); eip != nil && eip.Equal(ip) {
				return true
			}
		case "cidr":
			if n := strings.TrimSpace(e.Net); n != "" {
				if _, ipnet, err := net.ParseCIDR(n); err == nil && ipnet.Contains(ip) {
					return true
				}
			}
		case "root_domain", "subdomain":
			d := strings.ToLower(strings.TrimSpace(e.Domain))
			if d == "" {
				continue
			}
			matched := (kind == "root_domain" && (low == d || strings.HasSuffix(low, "."+d))) ||
				(kind == "subdomain" && low == d)
			if !matched {
				continue
			}
			// VIII.4.5.5: hostname matching never authorizes an arbitrary
			// address. A hostname-kind entry only authorizes addresses in the
			// public-unicast class. Special-use addresses (private, loopback,
			// link-local, multicast, unspecified) need an explicit ip/cidr entry.
			a, ok := netip.AddrFromSlice(ip)
			if !ok || !a.IsValid() {
				continue
			}
			a = a.Unmap()
			if a.IsGlobalUnicast() && !a.IsPrivate() && !a.IsLoopback() &&
				!a.IsLinkLocalUnicast() && !a.IsLinkLocalMulticast() &&
				!a.IsMulticast() && !a.IsUnspecified() {
				return true
			}
		}
	}
	return false
}

func scopeSummary(entries []db.TaskScope) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		switch {
		case e.Domain != "":
			parts = append(parts, strings.TrimSpace(e.Domain))
		case e.Net != "":
			parts = append(parts, strings.TrimSpace(e.Net))
		case e.Value != "":
			parts = append(parts, strings.TrimSpace(e.Value))
		}
	}
	if len(parts) == 0 {
		return "(none declared)"
	}
	return strings.Join(parts, ", ")
}

// scopeGuard is the single chokepoint for target-assessment egress. VIII.4.2:
// it resolves the scope of the task executing in ctx (RunInfo) so that two
// overlapping tasks can never authorize each other. Call it in doHTTP before
// the client runs.
func scopeGuard(ctx context.Context, rawURL string) error {
	return CheckScopeForKey(scopeKeyFromContext(ctx), rawURL)
}
