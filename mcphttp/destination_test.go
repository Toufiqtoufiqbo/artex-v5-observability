package mcphttp

import (
	"bufio"
	"strings"
	"testing"
)

func withMCPAllowlist(t *testing.T, hosts []string) {
	t.Helper()
	destMu.RLock()
	oldAllowed, oldRestricted := destAllowed, destRestricted
	destMu.RUnlock()
	SetAllowedHosts(hosts)
	t.Cleanup(func() {
		destMu.Lock()
		destAllowed, destRestricted = oldAllowed, oldRestricted
		destMu.Unlock()
	})
}

func TestValidateDestinationPolicy(t *testing.T) {
	withMCPAllowlist(t, []string{"mcp.example.com:443", "127.0.0.1:9999"})
	for _, u := range []string{
		"https://mcp.example.com/mcp",
		"http://127.0.0.1:9999/sse", // explicit operator opt-in for a private endpoint
	} {
		if err := ValidateDestination(u); err != nil {
			t.Errorf("allowlisted destination %q rejected: %v", u, err)
		}
	}
	for _, u := range []string{
		"https://evil.example/mcp",
		"https://mcp.example.com:8443/mcp", // host entry was port-scoped
		"http://169.254.169.254/latest/meta-data/",
		"http://localhost:9999/mcp",
		"ftp://mcp.example.com/mcp",
		"https://user:pass@mcp.example.com/mcp",
	} {
		if err := ValidateDestination(u); err == nil {
			t.Errorf("destination %q should be denied", u)
		}
	}
}

func TestSSEEndpointRejectsUnlistedDestination(t *testing.T) {
	withMCPAllowlist(t, []string{"mcp.example.com"})
	r := bufio.NewReader(strings.NewReader(
		"event: endpoint\ndata: http://169.254.169.254:80/message?sessionId=x\n\n"))
	if _, err := readSSEEndpoint(r, "https://mcp.example.com/sse"); err == nil {
		t.Fatal("a server-announced metadata endpoint must be rejected")
	}
}
