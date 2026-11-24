package agent

import (
	"net"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// VIII.4.5.5 — fail-closed DNS answer policy.

func TestSelectAuthorizedIP_MixedRejectsWholeSet(t *testing.T) {
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(301, []db.TaskScope{{Kind: "ip", Net: "127.0.0.2/32"}})
	key := scopeKeyForTask(301)
	addrs := []net.IPAddr{{IP: net.ParseIP("127.0.0.3")}, {IP: net.ParseIP("127.0.0.2")}}
	if _, err := selectAuthorizedIP(key, "fixture", addrs); err == nil {
		t.Fatalf("mixed allowed/prohibited answers must be rejected entirely")
	}
	// Permutation must not change the security result.
	addrs = []net.IPAddr{{IP: net.ParseIP("127.0.0.2")}, {IP: net.ParseIP("127.0.0.3")}}
	if _, err := selectAuthorizedIP(key, "fixture", addrs); err == nil {
		t.Fatalf("answer ordering must not change the denial")
	}
}

func TestSelectAuthorizedIP_EmptyAndAllAuthorized(t *testing.T) {
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(301, []db.TaskScope{{Kind: "ip", Net: "127.0.0.2/32"}})
	key := scopeKeyForTask(301)
	if _, err := selectAuthorizedIP(key, "fixture", nil); err == nil {
		t.Fatal("empty answer set must be denied")
	}
	ip, err := selectAuthorizedIP(key, "fixture", []net.IPAddr{{IP: net.ParseIP("127.0.0.2")}, {IP: net.ParseIP("127.0.0.2")}})
	if err != nil || !ip.Equal(net.ParseIP("127.0.0.2")) {
		t.Fatalf("all-authorized answers: ip=%v err=%v", ip, err)
	}
}

func TestRootDomainDoesNotAuthorizeSpecialAddresses(t *testing.T) {
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(301, []db.TaskScope{{Kind: "root_domain", Domain: "example.com"}})
	key := scopeKeyForTask(301)
	for _, s := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "224.0.0.1", "0.0.0.0", "::1", "fe80::1"} {
		if addressAuthorized(key, net.ParseIP(s), "www.example.com") {
			t.Errorf("root_domain must not authorize special address %s", s)
		}
	}
}

func TestHostOfBracketedIPv6(t *testing.T) {
	if got := hostOf("http://[::1]:8080/x"); got != "::1" {
		t.Fatalf("hostOf bracketed IPv6 = %q, want ::1", got)
	}
	if got := hostOf("http://10.0.0.5:3000/x"); got != "10.0.0.5" {
		t.Fatalf("hostOf IPv4:port = %q", got)
	}
}
