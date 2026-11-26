package mcphttp

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
)

// Remote MCP endpoints receive the headers stored in the MCP row (typically a
// bearer token or API key). An operator can install an explicit destination
// allowlist with ARTEX_MCP_ALLOWED_HOSTS ("host" or "host:port",
// comma-separated). When it is set, every URL this client dials — configured
// URL, server-announced SSE message endpoint and HTTP redirects — must match
// an entry, and DNS answers are pinned to non-private addresses unless the
// hostname itself was explicitly allowlisted. With no allowlist the legacy
// behavior is kept for existing deployments.
var (
	destMu         sync.RWMutex
	destAllowed    map[string]struct{}
	destRestricted bool
)

// SetAllowedHosts installs the operator-controlled MCP destination allowlist.
func SetAllowedHosts(hosts []string) {
	m := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		if s := canonicalMCPHost(h); s != "" {
			m[s] = struct{}{}
		}
	}
	destMu.Lock()
	destAllowed = m
	destRestricted = len(m) > 0
	destMu.Unlock()
}

// AllowedHosts returns a copy of the installed allowlist.
func AllowedHosts() []string {
	destMu.RLock()
	defer destMu.RUnlock()
	out := make([]string, 0, len(destAllowed))
	for h := range destAllowed {
		out = append(out, h)
	}
	return out
}

// AllowedHostsFromEnv reads the operator allowlist from the environment.
func AllowedHostsFromEnv() []string {
	var out []string
	for _, part := range strings.Split(os.Getenv("ARTEX_MCP_ALLOWED_HOSTS"), ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func canonicalMCPHost(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

var forbiddenMCPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("192.0.0.192/32"),
}

func mcpIPForbidden(ip netip.Addr) bool {
	a := ip.Unmap()
	if !a.IsValid() || a.IsUnspecified() || a.IsLoopback() || a.IsMulticast() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsPrivate() {
		return true
	}
	for _, p := range forbiddenMCPPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func mcpHostForbidden(host string) bool {
	name := strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	switch name {
	case "", "localhost", "localhost.localdomain", "metadata", "metadata.google.internal",
		"instance-data", "metadata.goog", "metadata.azure.com":
		return true
	}
	if ip, err := netip.ParseAddr(name); err == nil {
		return mcpIPForbidden(ip)
	}
	return false
}

func mcpURLParts(raw string) (host, port, scheme string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", "", err
	}
	scheme = strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", "", fmt.Errorf("mcp destination: unsupported scheme %q", scheme)
	}
	if u.User != nil {
		return "", "", "", fmt.Errorf("mcp destination: credentials in URL are not allowed")
	}
	host = strings.ToLower(u.Hostname())
	port = u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host, port, scheme, nil
}

func hostAllowedLocked(host, port string) bool {
	if _, ok := destAllowed[strings.ToLower(host)+":"+port]; ok {
		return true
	}
	_, ok := destAllowed[strings.ToLower(host)]
	return ok
}

// ValidateDestination is the preflight used by API handlers before persisting
// an MCP URL. Client construction re-checks it at dial time.
func ValidateDestination(raw string) error { return validateDestination(raw) }

// validateDestination enforces the operator allowlist on one URL. Errors name
// only the host — never headers or credentials.
func validateDestination(raw string) error {
	destMu.RLock()
	defer destMu.RUnlock()
	if !destRestricted {
		return nil
	}
	host, port, _, err := mcpURLParts(raw)
	if err != nil {
		return err
	}
	// A metadata address is never an MCP endpoint: those services hand out
	// instance credentials, so they are denied even if explicitly allowlisted.
	if isMetadataAddress(host) {
		return fmt.Errorf("mcp destination %q is a metadata address (DENY)", host)
	}
	if !hostAllowedLocked(host, port) {
		return fmt.Errorf("mcp destination %q is not in ARTEX_MCP_ALLOWED_HOSTS (DENY)", host)
	}
	return nil
}

// isMetadataAddress recognizes link-local and well-known vendor metadata IPs
// plus their canonical hostnames. Unlike general private addresses these are
// always denied: they expose cloud instance credentials.
func isMetadataAddress(host string) bool {
	name := strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	switch name {
	case "metadata", "metadata.google.internal", "instance-data", "metadata.goog", "metadata.azure.com":
		return true
	}
	if ip, err := netip.ParseAddr(name); err == nil {
		for _, p := range forbiddenMCPPrefixes {
			if p.Contains(ip.Unmap()) {
				return true
			}
		}
	}
	return false
}

// restrictedDial pins each dial to a public DNS answer. An explicitly
// allowlisted hostname may resolve to a private address — the operator made
// that choice — but an unlisted private answer is never dialed.
func restrictedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	host = strings.Trim(host, "[]")
	destMu.RLock()
	explicit := hostAllowedLocked(host, port)
	destMu.RUnlock()
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if !explicit && mcpIPForbidden(ip) {
			return nil, fmt.Errorf("mcp destination %s is a private/metadata address (DENY)", ip)
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("mcp destination resolution failed for %q (DENY)", host)
	}
	var chosen netip.Addr
	for _, a := range addrs {
		ip, ok := netip.AddrFromSlice(a.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		if !explicit && mcpIPForbidden(ip) {
			continue
		}
		chosen = ip
		break
	}
	if !chosen.IsValid() {
		return nil, fmt.Errorf("mcp destination %q resolves only to private/metadata addresses (DENY)", host)
	}
	return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(chosen.String(), port))
}
