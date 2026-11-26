package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// VIII.4.4 — Provider Egress enforcement at the actual ARTEX transport
// boundary.
//
// The Norma module (github.com/Autumn-27/norma) is external and is not
// modified. Instead, ARTEX wraps the provider HTTP client's transport with a
// provider-egress check: every request the client sends passes through
// providerEgressCheckTransport before hitting the network. The policy is
// typed as ProviderEgressPolicy and describes the exact, expected provider
// destination (scheme/host/port); there is no wildcard external authorization.
//
// Policy sources, in order of precedence:
//  1. Per-provider policy derived from the configured BaseURL (NewProvider).
//  2. Global override via SetProviderEgressPolicy.
// If neither authorizes a request, it is denied ("provider egress: not
// configured (DENY)").
// ---------------------------------------------------------------------------

// ProviderEgressPolicy describes the exact authorized provider destination.
// Missing ExpectedHost → the policy denies all requests.
type ProviderEgressPolicy struct {
	ExpectedHost   string
	ExpectedPort   string // "" → derived from scheme (443 https / 80 http)
	ExpectedScheme string // "" → "https"
	// AllowedPrefixes, when non-empty, restricts which resolved addresses the
	// trusted provider host may be pinned to (VIII.4.6 Gate A3). Every DNS
	// answer must fall inside at least one prefix; a mixed answer set is
	// rejected wholesale (fail closed). Empty = the trusted host's answers are
	// accepted as-is (the hostname is still the authorization anchor).
	AllowedPrefixes []netip.Prefix
	ExtraValidators []func(req *http.Request) error
}

var (
	providerEgressMu      sync.RWMutex
	providerEgressTrusted *ProviderEgressPolicy
	// providerTrustedHosts is the operator's trusted provider host set
	// (VIII.4.6 Gate A2). When non-empty, a BaseURL-derived policy may only
	// name a host in this set: a runtime/UI BaseURL cannot authorize a new
	// provider identity by itself. Empty = no restriction (explicitly
	// constructed clients, tests).
	providerTrustedHosts map[string]struct{}
	providerHostsSeeded  bool
)

// SetTrustedProviderHosts replaces the operator trusted-provider host set.
// Hosts are canonicalized (lowercase, trimmed); empty entries are ignored.
// Loopback/private literals such as 127.0.0.1:8899 are accepted here because
// listing them is an explicit operator opt-in for a local relay. Metadata
// destinations are never accepted.
func SetTrustedProviderHosts(hosts []string) {
	m := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		if s := canonicalProviderHost(h); s != "" && !providerMetadataForbidden(s) {
			m[s] = struct{}{}
		}
	}
	providerEgressMu.Lock()
	providerTrustedHosts = m
	providerHostsSeeded = true
	providerEgressMu.Unlock()
}

// SeedTrustedProviderHosts installs the trusted host set once (first call
// wins). Startup uses this so a later runtime BaseURL cannot widen it.
func SeedTrustedProviderHosts(hosts ...string) {
	providerEgressMu.Lock()
	if providerHostsSeeded {
		providerEgressMu.Unlock()
		return
	}
	m := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		if s := canonicalProviderHost(h); s != "" && !providerMetadataForbidden(s) {
			m[s] = struct{}{}
		}
	}
	providerTrustedHosts = m
	providerHostsSeeded = true
	providerEgressMu.Unlock()
}

// TrustedProviderHostsFromEnv returns the operator trusted host set from
// ARTEX_TRUSTED_PROVIDER_HOSTS (comma-separated "host" or "host:port"),
// falling back to the host:port of ARTEX_LLM_BASE_URL when set. Metadata
// destinations are always discarded. Loopback/private entries in the explicit
// operator list are accepted (a deliberate local relay opt-in); the fallback
// derived from ARTEX_LLM_BASE_URL still drops private/metadata destinations by
// default so merely configuring a base URL cannot create a trust root.
func TrustedProviderHostsFromEnv() []string {
	var out []string
	for _, part := range strings.Split(os.Getenv("ARTEX_TRUSTED_PROVIDER_HOSTS"), ",") {
		if s := strings.TrimSpace(part); s != "" && !providerMetadataForbidden(s) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		if host := canonicalProviderHost(urlHostName(os.Getenv("ARTEX_LLM_BASE_URL"))); host != "" && !providerHostDefaultDenied(host) {
			if port := urlPortOf(os.Getenv("ARTEX_LLM_BASE_URL")); port != "" {
				out = append(out, host+":"+port)
			} else {
				out = append(out, host)
			}
		}
	}
	return out
}

// TrustedProviderHosts returns the current trusted host set (sorted-free).
func TrustedProviderHosts() []string {
	providerEgressMu.RLock()
	defer providerEgressMu.RUnlock()
	out := make([]string, 0, len(providerTrustedHosts))
	for h := range providerTrustedHosts {
		out = append(out, h)
	}
	return out
}

// canonicalProviderHost is the single hostname canonicalization used by both
// policy construction and dial-time authorization: lowercase + trim only.
// A trailing dot is deliberately NOT stripped — it is a distinct hostname and
// therefore denied unless the policy names it exactly (conservative).
func canonicalProviderHost(h string) string {
	return strings.ToLower(strings.TrimSpace(h))
}

// forbiddenProviderPrefixes covers metadata destinations that are never
// legitimate LLM provider endpoints: link-local (including 169.254.169.254)
// and well-known vendor metadata IPs outside that range.
var forbiddenProviderPrefixes = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("192.0.0.192/32"),
}

// providerMetadataIPForbidden reports whether ip is a metadata/non-routable
// address that must be denied unconditionally. Loopback and RFC1918 addresses
// are deliberately NOT here: an operator may explicitly trust a local relay.
func providerMetadataIPForbidden(ip netip.Addr) bool {
	a := ip.Unmap()
	if !a.IsValid() || a.IsUnspecified() || a.IsMulticast() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() {
		return true
	}
	for _, p := range forbiddenProviderPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// providerDefaultIPDenied reports whether ip is denied unless the operator
// explicitly listed it as a trusted provider destination.
func providerDefaultIPDenied(ip netip.Addr) bool {
	a := ip.Unmap()
	return providerMetadataIPForbidden(a) || a.IsLoopback() || a.IsPrivate()
}

// providerHostName reduces "host" or "host:port" to a canonical host name.
func providerHostName(host string) string {
	h := canonicalProviderHost(host)
	name := h
	if only, _, err := net.SplitHostPort(h); err == nil {
		name = only
	}
	return strings.ToLower(strings.Trim(name, "[]"))
}

// providerMetadataForbidden checks well-known metadata names and literals.
// These destinations stay denied even if an operator mistakenly lists them;
// connection-test requests carry the real provider API key.
func providerMetadataForbidden(host string) bool {
	name := providerHostName(host)
	switch name {
	case "metadata", "metadata.google.internal", "instance-data", "metadata.goog",
		"metadata.azure.com":
		return true
	}
	if ip, err := netip.ParseAddr(name); err == nil {
		return providerMetadataIPForbidden(ip)
	}
	return false
}

// providerHostDefaultDenied checks the fail-closed default: metadata,
// loopback, private and other non-routable destinations are not trusted
// implicitly. An explicit entry in the operator trusted set overrides this for
// loopback/private hosts; metadata remains denied via providerMetadataForbidden.
func providerHostDefaultDenied(host string) bool {
	name := providerHostName(host)
	if name == "localhost" || name == "localhost.localdomain" {
		return true
	}
	if providerMetadataForbidden(host) {
		return true
	}
	if ip, err := netip.ParseAddr(name); err == nil {
		return providerDefaultIPDenied(ip)
	}
	return false
}

// validProviderHost accepts only ASCII DNS-ish hostnames and IP literals.
// Non-ASCII/IDNA input is rejected rather than silently normalized.
func validProviderHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-', c == '.', c == ':', c == '_':
		default:
			return false
		}
	}
	return true
}

// providerPolicyPort returns the authorized port, deriving it from the scheme
// when the policy omits it.
func providerPolicyPort(p ProviderEgressPolicy) string {
	if port := strings.TrimSpace(p.ExpectedPort); port != "" {
		return port
	}
	switch strings.ToLower(strings.TrimSpace(p.ExpectedScheme)) {
	case "http":
		return "80"
	case "", "https":
		return "443"
	}
	return ""
}

// addressInProviderPrefixes reports whether ip is permitted by the optional
// prefix allowlist. Empty allowlist = every answer permitted (hostname anchor).
func addressInProviderPrefixes(ip net.IP, prefixes []netip.Prefix) bool {
	if len(prefixes) == 0 {
		return true
	}
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// SetTrustedProviderPolicy installs the independently trusted provider
// destination policy (VIII.4.4.1, Phase B). It takes precedence over the
// per-provider BaseURL-derived policy — authorization is NOT derived from the
// endpoint being authorized. enabled=false or empty host → clears → deny.
func SetTrustedProviderPolicy(p ProviderEgressPolicy, enabled bool) {
	providerEgressMu.Lock()
	defer providerEgressMu.Unlock()
	if !enabled || strings.TrimSpace(p.ExpectedHost) == "" || providerMetadataForbidden(p.ExpectedHost) {
		providerEgressTrusted = nil
		return
	}
	pp := p
	providerEgressTrusted = &pp
}

func trustedProviderPolicyLoaded() *ProviderEgressPolicy {
	providerEgressMu.RLock()
	defer providerEgressMu.RUnlock()
	if providerEgressTrusted == nil {
		return nil
	}
	p := *providerEgressTrusted
	return &p
}

// providerEgressPolicyFor returns the effective policy for a client built from
// baseURL, in precedence order: trusted policy (independent of BaseURL),
// then BaseURL-derived — but only when the BaseURL host is inside the
// operator's trusted host set (when that set is non-empty). Fail-closed: an
// empty host or an untrusted BaseURL denies all requests.
func providerEgressPolicyFor(baseURL string) (ProviderEgressPolicy, bool) {
	if p := trustedProviderPolicyLoaded(); p != nil {
		if providerMetadataForbidden(p.ExpectedHost) {
			return ProviderEgressPolicy{}, false
		}
		return *p, true
	}
	host := canonicalProviderHost(urlHostName(baseURL))
	if host != "" && providerMetadataForbidden(host) {
		return ProviderEgressPolicy{}, false
	}
	providerEgressMu.RLock()
	restricted := len(providerTrustedHosts) > 0
	_, trusted := providerTrustedHosts[host]
	providerEgressMu.RUnlock()
	if restricted && !trusted {
		return ProviderEgressPolicy{}, false
	}
	return ProviderEgressPolicy{
		ExpectedHost:   host,
		ExpectedPort:   urlPortOf(baseURL),
		ExpectedScheme: strings.ToLower(strings.TrimSpace(urlSchemeOf(baseURL))),
	}, true
}

func urlHostName(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func urlPortOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}

func urlSchemeOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Scheme
}

// CheckProviderEgress validates a request against the policy. Returns a
// classified error (see EgressEvent decisions).
func CheckProviderEgress(req *http.Request, policy ProviderEgressPolicy) error {
	wantHost := strings.ToLower(strings.TrimSpace(policy.ExpectedHost))
	if wantHost == "" {
		return fmt.Errorf("provider egress: expected host missing (DENY)")
	}
	gotHost := strings.ToLower(req.URL.Hostname())
	if providerMetadataForbidden(wantHost) || providerMetadataForbidden(gotHost) {
		return fmt.Errorf("provider egress denied: metadata destinations are never allowed (DENY)")
	}
	if gotHost != wantHost {
		return fmt.Errorf("provider egress denied: host %q not in policy (expected %q)", gotHost, wantHost)
	}
	wantScheme := strings.ToLower(strings.TrimSpace(policy.ExpectedScheme))
	if wantScheme == "" {
		wantScheme = "https"
	}
	gotScheme := strings.ToLower(req.URL.Scheme)
	if gotScheme != wantScheme {
		return fmt.Errorf("provider egress denied: scheme %q not in policy (expected %q)", gotScheme, wantScheme)
	}
	wantPort := strings.TrimSpace(policy.ExpectedPort)
	if wantPort == "" {
		if wantScheme == "https" {
			wantPort = "443"
		} else if wantScheme == "http" {
			wantPort = "80"
		}
	}
	gotPort := req.URL.Port()
	if gotPort == "" {
		switch gotScheme {
		case "https":
			gotPort = "443"
		case "http":
			gotPort = "80"
		}
	}
	if wantPort != "" && gotPort != wantPort {
		return fmt.Errorf("provider egress denied: port %q not in policy (expected %q)", gotPort, wantPort)
	}
	for _, v := range policy.ExtraValidators {
		if err := v(req); err != nil {
			return err
		}
	}
	return nil
}

// TrustedProviderHostsRestricted reports whether the operator installed a
// non-empty trusted provider host set (VIII.4.7 Gate A).
func TrustedProviderHostsRestricted() bool {
	providerEgressMu.RLock()
	defer providerEgressMu.RUnlock()
	return len(providerTrustedHosts) > 0
}

// ProviderPolicyForBaseURL computes the trusted egress policy for one provider
// BaseURL from trusted configuration only (VIII.4.6 Gate B). A global trusted
// policy is honored only when it names the same host; otherwise the BaseURL
// host must be in the operator's trusted host set. Unlike the legacy
// NewProvider path, an EMPTY trusted set is NOT permissive here: this function
// is the pre-flight used by /api/llm/test, which attaches the stored global API
// key to a caller-supplied URL. No explicit operator trust → no request.
// Loopback/private hosts are permitted only when explicitly present in the
// trusted set; metadata destinations are never permitted. ok=false means the
// caller must not construct a provider for this BaseURL.
func ProviderPolicyForBaseURL(baseURL string) (ProviderEgressPolicy, bool) {
	host := canonicalProviderHost(urlHostName(baseURL))
	// An explicit trusted policy is independent of the user-supplied URL, so it
	// is honored first (including for an empty default BaseURL). This keeps the
	// trusted-policy path usable without turning a caller-supplied host into a
	// trust root.
	if p := trustedProviderPolicyLoaded(); p != nil {
		if providerMetadataForbidden(p.ExpectedHost) {
			return ProviderEgressPolicy{}, false
		}
		if host == "" || canonicalProviderHost(p.ExpectedHost) == host {
			return *p, true
		}
	}
	if host == "" || !validProviderHost(host) || providerMetadataForbidden(host) {
		return ProviderEgressPolicy{}, false
	}
	port := urlPortOf(baseURL)
	scheme := strings.ToLower(strings.TrimSpace(urlSchemeOf(baseURL)))
	providerEgressMu.RLock()
	restricted := len(providerTrustedHosts) > 0
	allowed := hostTrustedLocked(host, port)
	providerEgressMu.RUnlock()
	// hostTrustedLocked is the explicit operator opt-in, so an otherwise
	// private/loopback host is authorized here. Unlisted hosts (including local
	// ones) still fail closed, and metadata was rejected above.
	if !restricted || !allowed {
		return ProviderEgressPolicy{}, false
	}
	return ProviderEgressPolicy{ExpectedHost: host, ExpectedPort: port, ExpectedScheme: scheme}, true
}

// hostTrustedLocked matches host against the operator set. An entry may be
// "host" (any port on that host) or "host:port" (exact port). Caller holds
// providerEgressMu.
func hostTrustedLocked(host, port string) bool {
	if len(providerTrustedHosts) == 0 {
		return false
	}
	if _, ok := providerTrustedHosts[host]; ok {
		return true
	}
	if port != "" {
		_, ok := providerTrustedHosts[host+":"+port]
		return ok
	}
	return false
}

// CheckProviderBaseURLTrusted is the read-only pre-flight used by the server
// before persisting or activating a provider profile.
func CheckProviderBaseURLTrusted(baseURL string) error {
	if _, ok := ProviderPolicyForBaseURL(baseURL); !ok {
		return fmt.Errorf("provider egress: BaseURL host %q is not authorized by trusted provider configuration (DENY)", canonicalProviderHost(urlHostName(baseURL)))
	}
	return nil
}

// providerPinnedDial pins the actual provider TCP destination to an address
// resolved from the TRUSTED host (independent of BaseURL). VIII.4.6 Gate A:
// the dial hostname must equal the trusted host (identity, not IP equality),
// the port must match, and every DNS answer must satisfy the optional prefix
// policy. Resolver failures and mixed answer sets fail closed.
func providerPinnedDial(policy ProviderEgressPolicy, base func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return providerPinnedDialWithResolver(policy, base, func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return net.DefaultResolver.LookupIPAddr(ctx, host)
	})
}

// providerPinnedDialWithResolver is the request-scoped seam used by tests.
// No global resolver or pin state is stored.
func providerPinnedDialWithResolver(policy ProviderEgressPolicy,
	base func(context.Context, string, string) (net.Conn, error),
	resolve func(context.Context, string) ([]net.IPAddr, error),
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port := splitHostPort(addr)
		wantHost := canonicalProviderHost(policy.ExpectedHost)
		gotHost := canonicalProviderHost(host)
		if wantHost == "" {
			return nil, fmt.Errorf("provider egress: trusted host missing (DENY)")
		}
		if providerMetadataForbidden(wantHost) || providerMetadataForbidden(gotHost) {
			return nil, fmt.Errorf("provider egress: metadata destinations are never allowed (DENY)")
		}
		if gotHost == "" || !validProviderHost(gotHost) || gotHost != wantHost {
			return nil, fmt.Errorf("provider egress: dial host %q is not the trusted provider host %q (DENY)", host, policy.ExpectedHost)
		}
		if wantPort := providerPolicyPort(policy); wantPort != "" && port != wantPort {
			return nil, fmt.Errorf("provider egress: dial port %q is not authorized (expected %q) (DENY)", port, wantPort)
		}
		// An IP-literal trusted host is explicitly authorized by the policy
		// itself; still honor the prefix allowlist when one is configured.
		if ip := net.ParseIP(host); ip != nil {
			if !addressInProviderPrefixes(ip, policy.AllowedPrefixes) {
				return nil, fmt.Errorf("provider egress: literal %s outside the allowed prefixes (DENY)", ip)
			}
			return base(ctx, network, addr)
		}
		// One resolution, one authorization decision, one pinned dial. Resolving
		// the trusted and requested names separately would leave a rebinding
		// window between the two lookups.
		addrs, err := resolve(ctx, wantHost)
		if err != nil {
			return nil, fmt.Errorf("provider egress: DNS resolution failed for %q (DENY)", wantHost)
		}
		if len(addrs) == 0 {
			return nil, fmt.Errorf("provider egress: empty DNS answer for %q (DENY)", wantHost)
		}
		var selected net.IP
		for _, a := range addrs {
			if a.IP == nil {
				return nil, fmt.Errorf("provider egress: malformed DNS answer for %q (DENY)", wantHost)
			}
			if ipAddr, ok := netip.AddrFromSlice(a.IP); ok && providerMetadataIPForbidden(ipAddr) {
				return nil, fmt.Errorf("provider egress: DNS answer %s is a metadata destination (DENY)", a.IP)
			}
			if !addressInProviderPrefixes(a.IP, policy.AllowedPrefixes) {
				return nil, fmt.Errorf("provider egress: DNS answer %s for %q is not authorized (DENY)", a.IP, wantHost)
			}
			if selected == nil {
				selected = a.IP
			}
		}
		return base(ctx, network, net.JoinHostPort(selected.String(), port))
	}
}

// providerEgressCheckTransport wraps the provider client's base transport.
type providerEgressCheckTransport struct {
	base   http.RoundTripper
	policy ProviderEgressPolicy
}

func (t providerEgressCheckTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := CheckProviderEgress(req, t.policy); err != nil {
		emitEgressEvent(EgressEventForURL(EgressLLMProvider, 0, req.URL.String(), EgressDecisionDeny, err.Error()))
		return nil, err
	}
	emitEgressEvent(EgressEventForURL(EgressLLMProvider, 0, req.URL.String(), EgressDecisionAllow, "provider-egress"))
	return t.base.RoundTrip(req)
}
