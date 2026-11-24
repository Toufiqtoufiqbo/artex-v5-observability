package traffic

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
)

// VIII.4.3 — unified egress control, part 1: explicit destination allowlist
// for the traffic interception proxy.
//
// Without a configured allowlist the proxy keeps its previous behaviour (nil
// upstream = direct dial). With an allowlist, every upstream dial —
// intercepted, CONNECT-tunneled and transparent-passthrough — is checked
// against it by the hook in Open() before the connection is made. Unknown
// destinations are denied with an explicit error.

// SetEgressAllowlist sets the explicit outbound-destination allowlist
// (VIII.4.3/VIII.4.4). Keys are "host" or "host:port" (case-insensitive);
// both forms accepted as matches.
//
// Semantics (VIII.4.4):
//   - sealed=false → legacy behavior: an empty/unset allowlist is permissive
//     (`EgressAllowed` returns true); used only by tests/back-compat paths.
//   - sealed=true → the allowlist is authoritative: destinations must match an
//     entry; with an empty allowlist this DENIES EVERYTHING (fail closed).
//
// Hot-swappable at runtime.
func (t *Traffic) SetEgressAllowlist(hosts []string, sealed bool) {
	m := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		if s := strings.ToLower(strings.TrimSpace(h)); s != "" {
			m[s] = struct{}{}
		}
	}
	t.egressAllowlist.Store(m)
	t.egressSealed.Store(sealed)
}

// AllowlistFromEnv returns the operator's explicit egress allowlist from
// ARTEX_EGRESS_ALLOWLIST (comma-separated "host" or "host:port" entries), or
// nil when the variable is unset or empty. A non-nil (possibly empty) result
// means the operator explicitly configured the allowlist; callers must then
// treat nil-allowlist as deny-all (fail closed). Entries are normalized
// (lowercased, trimmed); empty/whitespace-only entries are dropped.
func AllowlistFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("ARTEX_EGRESS_ALLOWLIST"))
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if s := strings.ToLower(strings.TrimSpace(part)); s != "" {
			out = append(out, s)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// AllowedEgressHosts returns the current allowlist keys ("host"/"host:port").
func (t *Traffic) AllowedEgressHosts() []string {
	m, _ := t.egressAllowlist.Load().(map[string]struct{})
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// EgressAllowed reports whether the request's logical destination is allowed
// by the policy.
//   - unsealed (legacy): everything allowed.
//   - sealed: destination must match the allowlist ("host" or "host:port"
//     entry, case-insensitive); empty allowlist → deny all; empty host → deny.
func (t *Traffic) EgressAllowed(req *http.Request) bool {
	if !t.egressSealed.Load() {
		return true
	}
	m, _ := t.egressAllowlist.Load().(map[string]struct{})
	host := egressDestHost(req)
	if host == "" {
		return false
	}
	if _, ok := m[host]; ok {
		return true
	}
	// Accept the host-only form when the entry carries a port (and vice versa).
	if h := hostOnly(host); h != "" && h != host {
		if _, ok := m[h]; ok {
			return true
		}
	}
	return false
}

// egressDestHost extracts the destination host:port from a proxy request,
// tolerating both "host:port" (CONNECT) and full-URL forms.
func egressDestHost(req *http.Request) string {
	if req == nil {
		return ""
	}
	h := strings.TrimSpace(req.Host)
	if h == "" && req.URL != nil {
		h = strings.TrimSpace(req.URL.Host)
	}
	if h == "" {
		return ""
	}
	return strings.ToLower(h)
}

// EgressAllowedAddr is the address-level form of EgressAllowed used by the
// pinned upstream forwarder (VIII.4.6 Gate D). It accepts a "host" or
// "host:port" allowlist entry; unsealed mode allows everything (legacy).
func (t *Traffic) EgressAllowedAddr(host, port string) bool {
	if !t.egressSealed.Load() {
		return true
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	m, _ := t.egressAllowlist.Load().(map[string]struct{})
	if _, ok := m[host]; ok {
		return true
	}
	if port != "" {
		if _, ok := m[host+":"+port]; ok {
			return true
		}
		if _, ok := m[net.JoinHostPort(host, port)]; ok {
			return true
		}
	}
	return false
}

// upstreamProxyFor is the go-mitmproxy SetUpstreamProxy hook. It is the only
// supported pre-dial extension point in go-mitmproxy v1.9.2.
func (t *Traffic) upstreamProxyFor(req *http.Request) (*url.URL, error) {
	host := egressDestHost(req)
	if !t.EgressAllowed(req) {
		err := fmt.Errorf("traffic proxy: egress to %s denied by explicit allowlist", host)
		emitTrafficEgressAudit(host, false, err.Error())
		return nil, err
	}
	if !t.egressSealed.Load() {
		// Legacy/unsealed: preserve the previous direct-or-chained behavior.
		emitTrafficEgressAudit(host, true, "")
		return t.upstream.Load(), nil
	}
	if t.upstream.Load() != nil {
		err := fmt.Errorf("traffic proxy: chained upstream proxy is incompatible with sealed pinned egress (DENY)")
		emitTrafficEgressAudit(host, false, err.Error())
		return nil, err
	}
	emitTrafficEgressAudit(host, true, "pinned-forwarder")
	if t.forwarder == nil {
		return nil, fmt.Errorf("traffic proxy: pinned forwarder not initialized (DENY)")
	}
	return t.forwarder.url(), nil
}

// SetEgressAddressPolicy installs an optional address-prefix allowlist for the
// pinned upstream dial (VIII.4.6 Gate D). When enabled with a non-empty set,
// every resolved answer must fall inside a prefix; a mixed answer set is
// rejected wholesale. enabled=false clears the restriction (hostname anchor).
func (t *Traffic) SetEgressAddressPolicy(cidrs []string, enabled bool) error {
	var ps []netip.Prefix
	if enabled {
		for _, c := range cidrs {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			p, err := netip.ParsePrefix(c)
			if err != nil {
				return fmt.Errorf("traffic egress: invalid address prefix %q: %w", c, err)
			}
			ps = append(ps, p)
		}
	}
	t.egressPrefixes.Store(ps)
	return nil
}

func (t *Traffic) egressPrefixValues() []netip.Prefix {
	v, _ := t.egressPrefixes.Load().([]netip.Prefix)
	return v
}

// VIII.4.3 — Phase I: structured audit events for traffic-proxy egress
// decisions. Secret-safe: host only, plus allow/deny and the deny reason.
var (
	egressAuditMu   sync.RWMutex
	egressAuditFunc func(host string, allowed bool, reason string)
)

// SetEgressAuditSink installs the audit callback for traffic-proxy egress
// decisions. Observational only: it never grants or denies.
func SetEgressAuditSink(fn func(host string, allowed bool, reason string)) {
	egressAuditMu.Lock()
	defer egressAuditMu.Unlock()
	egressAuditFunc = fn
}

func emitTrafficEgressAudit(host string, allowed bool, reason string) {
	egressAuditMu.RLock()
	fn := egressAuditFunc
	egressAuditMu.RUnlock()
	if fn != nil {
		fn(host, allowed, reason)
	}
}
