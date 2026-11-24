package agent

import (
	"net/http"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

// VIII.4.7 Gate A — provider trust-root integrity:
//
// The trusted provider host set must originate only from independently
// operator-controlled configuration (env/explicit seeds). A user-controlled
// BaseURL (e.g. a UI-saved LLM profile) must never seed or mutate trust, and
// the runtime config update path must not add trusted hosts.

func hasHost(set []string, h string) bool {
	for _, s := range set {
		if s == h {
			return true
		}
	}
	return false
}

func TestTrustSetFirstSeedWinsCannotWiden(t *testing.T) {
	resetProviderTrustedHosts(t)
	SeedTrustedProviderHosts("trusted.example")
	// A later seed attempt (e.g. from user-controlled input) must not widen.
	SeedTrustedProviderHosts("evil.example")
	set := TrustedProviderHosts()
	if len(set) != 1 || !hasHost(set, "trusted.example") {
		t.Fatalf("trust set = %v, want exactly [trusted.example] (first seed wins)", set)
	}
	if hasHost(set, "evil.example") {
		t.Fatal("runtime seed attempt widened the operator trust set")
	}
}

func TestUserControlledProfileBaseURLCannotSeedTrust(t *testing.T) {
	resetProviderTrustedHosts(t)
	SeedTrustedProviderHosts("operator.example")
	// Simulate a runtime attempt to trust the BaseURL of a user-controlled
	// profile (the removed loadLLMConfig bootstrap, now deleted): the seed
	// primitive itself must remain blocked after the first seed, and the
	// policy must not allow the user-controlled host.
	SeedTrustedProviderHosts("saved-by-user.example")
	set := TrustedProviderHosts()
	if len(set) != 1 || !hasHost(set, "operator.example") {
		t.Fatalf("trust set = %v, want [operator.example]", set)
	}
	if _, ok := ProviderPolicyForBaseURL("https://saved-by-user.example/v1"); ok {
		t.Fatal("user-controlled profile host must not become trusted")
	}
	if _, ok := ProviderPolicyForBaseURL("https://operator.example/v1"); !ok {
		t.Fatal("operator-trusted host must remain trusted")
	}
}

func TestRestrictedTrustPolicyPermutations(t *testing.T) {
	resetProviderTrustedHosts(t)
	SetTrustedProviderHosts([]string{"trusted.example:8443"})
	allow := []string{
		"https://trusted.example:8443/v1",         // exact host + exact port
		"http://TRUSTED.EXAMPLE:8443/v1",          // case canonicalization
		"https://trusted.example:8443/",           // trailing slash
	}
	deny := []string{
		"https://evil.example/v1",                // unlisted host
		"https://sub.trusted.example:8443/v1",    // subdomain of a port-scoped entry
		"https://parent.example/trusted.example", // parent alias (not exact)
		"https://trusted.example/v1",             // host without the operator port
		"https://trusted.example:9443/v1",        // different port
		"https://trusted.example.:8443/v1",       // trailing-dot alias (canonical keeps dot)
	}
	for _, u := range allow {
		if _, ok := ProviderPolicyForBaseURL(u); !ok {
			t.Errorf("must be allowed: %q", u)
		}
	}
	for _, u := range deny {
		if _, ok := ProviderPolicyForBaseURL(u); ok {
			t.Errorf("must be denied: %q", u)
		}
	}
}

func TestSetTrustedProviderHostsMalformedEntriesNeverAuthorize(t *testing.T) {
	resetProviderTrustedHosts(t)
	SetTrustedProviderHosts([]string{"https://evil.com/path", "ex ample", ""})
	if _, ok := ProviderPolicyForBaseURL("https://evil.com/v1"); ok {
		t.Fatal("malformed trust entries must fail closed (never match a canonical host)")
	}
	if _, ok := ProviderPolicyForBaseURL("https://trusted.example/v1"); ok {
		t.Fatal("no operator host was configured, so nothing must be trusted")
	}
}

func TestCheckProviderEgressWrongSchemeDenied(t *testing.T) {
	resetProviderTrustedHosts(t)
	pol := ProviderEgressPolicy{ExpectedHost: "trusted.example", ExpectedPort: "443", ExpectedScheme: "https"}
	req, err := http.NewRequest("GET", "ftp://trusted.example:443/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckProviderEgress(req, pol); err == nil {
		t.Fatal("wrong-scheme request must be denied")
	}
}

func TestLegacyNewProviderFailsClosedWhenUntrusted(t *testing.T) {
	resetProviderTrustedHosts(t)
	SetTrustedProviderHosts([]string{"trusted.example"})
	// Legacy entry point must not build a client for an untrusted BaseURL.
	if _, err := (Config{BaseURL: "https://evil.example/v1"}).NewProvider(); err == nil {
		t.Fatal("NewProvider for untrusted BaseURL must fail closed")
	}
	// Trusted host constructs without error (no dial happens during build).
	if _, err := (Config{BaseURL: "https://trusted.example/v1", Model: "m", APIKey: "k", Format: llm.FormatOpenAI}).NewProvider(); err != nil {
		t.Fatalf("NewProvider for trusted BaseURL: %v", err)
	}
}
