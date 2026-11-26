package agent

import (
	"strings"
	"testing"
)

// Security hardening regression tests: the connection-test pre-flight must
// require explicit operator trust and must never authorize a local/metadata
// destination, even if one appears in the operator list.

func resetProviderTrustState(t *testing.T) {
	t.Helper()
	providerEgressMu.Lock()
	oldHosts, oldSeeded := providerTrustedHosts, providerHostsSeeded
	oldPolicy := providerEgressTrusted
	providerTrustedHosts, providerHostsSeeded, providerEgressTrusted = nil, false, nil
	providerEgressMu.Unlock()
	t.Cleanup(func() {
		providerEgressMu.Lock()
		providerTrustedHosts, providerHostsSeeded, providerEgressTrusted = oldHosts, oldSeeded, oldPolicy
		providerEgressMu.Unlock()
	})
}

func TestProviderPolicyForBaseURLRequiresExplicitTrust(t *testing.T) {
	resetProviderTrustState(t)
	if _, ok := ProviderPolicyForBaseURL("https://api.example.com/v1"); ok {
		t.Fatal("an unlisted BaseURL must not be authorized for a connection test")
	}
	SetTrustedProviderHosts([]string{"api.example.com"})
	if _, ok := ProviderPolicyForBaseURL("https://api.example.com/v1"); !ok {
		t.Fatal("an operator-trusted BaseURL must remain authorized")
	}
}

func TestProviderPolicyForBaseURLRejectsPrivateDestinations(t *testing.T) {
	resetProviderTrustState(t)
	SetTrustedProviderHosts([]string{
		"127.0.0.1:8443", "169.254.169.254", "localhost", "api.example.com",
	})
	for _, u := range []string{
		"https://127.0.0.1:8443/v1",
		"http://169.254.169.254/latest/meta-data/",
		"https://localhost/v1",
		"https://metadata.google.internal/v1",
	} {
		if _, ok := ProviderPolicyForBaseURL(u); ok {
			t.Errorf("private/metadata destination must be denied: %q", u)
		}
	}
	if _, ok := ProviderPolicyForBaseURL("https://api.example.com/v1"); !ok {
		t.Fatal("public operator-trusted host must still be authorized")
	}
}

func TestTrustedProviderHostsFromEnvDropsPrivateEntries(t *testing.T) {
	resetProviderTrustState(t)
	t.Setenv("ARTEX_TRUSTED_PROVIDER_HOSTS", "127.0.0.1:8443,169.254.169.254,api.example.com")
	t.Setenv("ARTEX_LLM_BASE_URL", "")
	got := TrustedProviderHostsFromEnv()
	if len(got) != 1 || !strings.EqualFold(got[0], "api.example.com") {
		t.Fatalf("trusted hosts = %v, want only [api.example.com]", got)
	}
}

func TestTrustedProviderHostsFromEnvDropsPrivateBaseURLFallback(t *testing.T) {
	resetProviderTrustState(t)
	t.Setenv("ARTEX_TRUSTED_PROVIDER_HOSTS", "")
	t.Setenv("ARTEX_LLM_BASE_URL", "http://169.254.169.254/latest/meta-data/")
	if got := TrustedProviderHostsFromEnv(); len(got) != 0 {
		t.Fatalf("metadata fallback was trusted: %v", got)
	}
}

func TestProviderPolicyHonorsExplicitTrustedPolicy(t *testing.T) {
	resetProviderTrustState(t)
	// Explicit operator policy is independent of the caller's BaseURL, so even
	// a local test endpoint can be pre-flighted when it was deliberately trusted.
	SetTrustedProviderPolicy(ProviderEgressPolicy{
		ExpectedHost: "127.0.0.1", ExpectedPort: "8443", ExpectedScheme: "https",
	}, true)
	for _, u := range []string{"https://127.0.0.1:8443/v1", ""} {
		if _, ok := ProviderPolicyForBaseURL(u); !ok {
			t.Errorf("explicitly trusted policy must authorize %q", u)
		}
	}
}
