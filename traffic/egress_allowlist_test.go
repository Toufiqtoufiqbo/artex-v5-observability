package traffic

import (
	"net/http"
	"strings"
	"testing"
)

// VIII.4.7 Gate B — traffic allowlist production wiring (unit tests).
//
// The allowlist is seeded only from the operator environment at startup.
// Malformed entries never authorize (fail closed), empty configuration
// denies all, and concurrent policy observation is consistent.

func newBareTraffic(t *testing.T) *Traffic {
	t.Helper()
	return &Traffic{}
}

func reqFor(t *testing.T, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestAllowlistFromEnv(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv("ARTEX_EGRESS_ALLOWLIST", "")
		if got := AllowlistFromEnv(); got != nil {
			t.Fatalf("unset must return nil, got %v", got)
		}
	})
	t.Run("empty", func(t *testing.T) {
		t.Setenv("ARTEX_EGRESS_ALLOWLIST", "   ")
		if got := AllowlistFromEnv(); got != nil {
			t.Fatalf("blank must return nil, got %v", got)
		}
	})
	t.Run("entries normalized", func(t *testing.T) {
		t.Setenv("ARTEX_EGRESS_ALLOWLIST", " Fixture.Example:8443 , bad,,  ,  ")
		got := AllowlistFromEnv()
		if len(got) != 2 || got[0] != "fixture.example:8443" || got[1] != "bad" {
			t.Fatalf("entries = %v, want [fixture.example:8443 bad]", got)
		}
	})
	t.Run("configured-but-all-empty", func(t *testing.T) {
		t.Setenv("ARTEX_EGRESS_ALLOWLIST", ",")
		if got := AllowlistFromEnv(); got == nil {
			t.Fatal("configured-but-empty must return non-nil (operator explicitly configured, deny-all)")
		}
	})
}

func TestSetEgressAllowlistMalformedEntriesNeverAuthorize(t *testing.T) {
	tr := newBareTraffic(t)
	tr.SetEgressAllowlist([]string{"https://evil.example/path", "ex ample", "127.0.0.1/32"}, true)
	if tr.EgressAllowed(reqFor(t, "http://evil.example/x")) {
		t.Fatal("malformed entry must not authorize evil.example")
	}
	if tr.EgressAllowed(reqFor(t, "http://127.0.0.1/32/x")) {
		t.Fatal("CIDR-form entry must not authorize host 127.0.0.1 (host/host:port only)")
	}
}

func TestSealedEmptyAllowlistDeniesAll(t *testing.T) {
	tr := newBareTraffic(t)
	tr.SetEgressAllowlist(nil, true)
	if !tr.egressSealed.Load() {
		t.Fatal("policy must be sealed")
	}
	if tr.EgressAllowed(reqFor(t, "http://fixture.example:19004/")) {
		t.Fatal("sealed-empty allowlist must deny all")
	}
}

func TestAllowedEntryMatchesHostAndHostPort(t *testing.T) {
	tr := newBareTraffic(t)
	tr.SetEgressAllowlist([]string{"fixture.example:8443"}, true)
	if !tr.EgressAllowed(reqFor(t, "http://fixture.example:8443/x")) {
		t.Fatal("listed host:port must be allowed")
	}
	if tr.EgressAllowed(reqFor(t, "http://fixture.example/x")) {
		t.Fatal("same host without the authorized port must be denied")
	}
	if tr.EgressAllowed(reqFor(t, "http://evil.example:8443/x")) {
		t.Fatal("unlisted host must be denied")
	}
}

func TestSetEgressAllowlistAcceptsHostOnlyEntry(t *testing.T) {
	tr := newBareTraffic(t)
	tr.SetEgressAllowlist([]string{"fixture.example"}, true)
	if !tr.EgressAllowed(reqFor(t, "http://fixture.example/x")) {
		t.Fatal("listed host must be allowed")
	}
	if !strings.Contains(strings.Join(tr.AllowedEgressHosts(), ","), "fixture.example") {
		t.Fatalf("allowlist keys = %v", tr.AllowedEgressHosts())
	}
}
