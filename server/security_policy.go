package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// Operator-controlled egress/destination policy for the two server-side
// features that hold credentials: custom-tool HTTP dry runs and remote MCP
// connections. None of these knobs are writable through the API — a user who
// can edit a tool row must not be able to turn a saved API key into an SSRF
// against cloud metadata or loopback services.

const (
	envCustomToolHTTPTestHosts = "ARTEX_CUSTOM_TOOL_HTTP_TEST_HOSTS"
	envMCPAllowedHosts         = "ARTEX_MCP_ALLOWED_HOSTS"
	envMCPInsecureHosts        = "ARTEX_MCP_INSECURE_HOSTS"

	// mcpEnvMask is what GET /api/mcp returns for an env/header value. It is
	// also accepted on write as "keep the stored value" so the edit form can
	// round-trip without clobbering a secret it never saw.
	mcpEnvMask = "********"
)

// sanitizeHTTPTestBody strips control characters (terminal/JSON escape tricks)
// and caps the bytes returned by an HTTP dry run.
func sanitizeHTTPTestBody(body string, max int) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < ' ' || r == 0x7f:
			return ' '
		default:
			return r
		}
	}, body)
	if max > 0 && len(clean) > max {
		return clean[:max] + "\n…(已截断)"
	}
	return clean
}

func operatorHostList(name string) []string {
	var out []string
	for _, part := range strings.Split(os.Getenv(name), ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, strings.ToLower(s))
		}
	}
	return out
}

var forbiddenPolicyPrefixes = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("192.0.0.192/32"),
}

func policyIPForbidden(ip netip.Addr) bool {
	a := ip.Unmap()
	if !a.IsValid() || a.IsUnspecified() || a.IsLoopback() || a.IsMulticast() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsPrivate() {
		return true
	}
	for _, p := range forbiddenPolicyPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// policyHostParts parses an operator-controlled URL without ever echoing its
// userinfo (credentials in URLs are rejected outright).
func policyHostParts(raw string) (host, port, scheme string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", "", err
	}
	scheme = strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", "", fmt.Errorf("unsupported scheme %q", scheme)
	}
	if u.User != nil {
		return "", "", "", fmt.Errorf("credentials in URL are not allowed")
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

func policyHostForbidden(host string) bool {
	name := strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	switch name {
	case "", "localhost", "localhost.localdomain", "metadata", "metadata.google.internal",
		"instance-data", "metadata.goog", "metadata.azure.com":
		return true
	}
	if ip, err := netip.ParseAddr(name); err == nil {
		return policyIPForbidden(ip)
	}
	return false
}

func hostInOperatorList(list []string, host, port string) bool {
	for _, entry := range list {
		if entry == host || entry == net.JoinHostPort(host, port) {
			return true
		}
	}
	return false
}

// checkCustomToolHTTPTestTarget gates the HTTP dry-run endpoint. With no
// operator allowlist every dry run is denied: the request can carry the saved
// tool's headers (API keys) and must never be aimed at an arbitrary URL.
func checkCustomToolHTTPTestTarget(rawURL string) error {
	hosts := operatorHostList(envCustomToolHTTPTestHosts)
	if len(hosts) == 0 {
		return fmt.Errorf("HTTP 测试未配置允许的目标主机（%s）", envCustomToolHTTPTestHosts)
	}
	host, port, _, err := policyHostParts(rawURL)
	if err != nil || policyHostForbidden(host) {
		return fmt.Errorf("HTTP 测试目标不被允许")
	}
	if !hostInOperatorList(hosts, host, port) {
		return fmt.Errorf("HTTP 测试目标不被允许")
	}
	return nil
}

// operatorMCPInsecure derives TLS-verification bypass solely from operator
// configuration. The stored `insecure` column is never trusted: it is writable
// from the UI and from an agent tool.
func operatorMCPInsecure(m *db.MCPServer) bool {
	if m == nil || m.Transport == "stdio" {
		return false
	}
	host, port, scheme, err := policyHostParts(m.URL)
	if err != nil || scheme != "https" {
		return false
	}
	return hostInOperatorList(operatorHostList(envMCPInsecureHosts), host, port)
}

// maskedMCPServer is the API view of an MCP row. Env keys stay visible so the
// edit form can show which headers are configured, but values are replaced by
// a fixed sentinel. Insecure reflects the operator's effective setting.
func maskedMCPServer(m *db.MCPServer) *db.MCPServer {
	if m == nil {
		return nil
	}
	cp := *m
	cp.Insecure = operatorMCPInsecure(m)
	cp.Env = json.RawMessage(`{}`)
	var kv map[string]string
	if json.Unmarshal(m.Env, &kv) == nil && len(kv) > 0 {
		masked := make(map[string]string, len(kv))
		for k := range kv {
			masked[k] = mcpEnvMask
		}
		if b, err := json.Marshal(masked); err == nil {
			cp.Env = b
		}
	}
	return &cp
}

// mergeMaskedMCPEnv keeps stored values for fields the client echoed back as
// the mask sentinel. A genuinely new value overwrites the stored one.
func mergeMaskedMCPEnv(existing, incoming json.RawMessage) json.RawMessage {
	var in map[string]string
	if err := json.Unmarshal(incoming, &in); err != nil || len(in) == 0 {
		return incoming
	}
	var old map[string]string
	_ = json.Unmarshal(existing, &old)
	out := make(map[string]string, len(in))
	for k, v := range in {
		if v == mcpEnvMask {
			if stored, ok := old[k]; ok {
				out[k] = stored
			}
			continue
		}
		out[k] = v
	}
	b, err := json.Marshal(out)
	if err != nil {
		return incoming
	}
	return b
}
