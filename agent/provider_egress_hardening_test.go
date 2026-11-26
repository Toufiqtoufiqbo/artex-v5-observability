package agent

import (
	"strings"
	"testing"
)

// Security hardening regression tests: the connection-test pre-flight must
// require explicit operator trust. Loopback/private destinations are allowed
// only when the operator explicitly lists them (a local relay opt-in);
// metadata destinations are never allowed, even when listed.

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

func TestProviderPolicyAllowsExplicitLocalRelayDeniesMetadata(t *testing.T) {
	resetProviderTrustState(t)
	SetTrustedProviderHosts([]string{
		"127.0.0.1:8899", "10.0.0.5", "169.254.169.254", "metadata.google.internal", "api.example.com",
	})
	for _, u := range []string{
		"http://127.0.0.1:8899/v1",   // explicitly trusted loopback relay
		"http://10.0.0.5/v1",         // explicitly trusted private relay
		"https://api.example.com/v1", // public host unchanged
	} {
		if _, ok := ProviderPolicyForBaseURL(u); !ok {
			t.Errorf("explicitly trusted destination must be allowed: %q", u)
		}
	}
	for _, u := range []string{
		"https://127.0.0.1:8898/v1",                // loopback, wrong port: not listed
		"https://localhost/v1",                     // loopback name: not listed
		"http://10.0.0.6/v1",                       // private host: not listed
		"http://169.254.169.254/latest/meta-data/", // metadata: always denied
		"https://metadata.google.internal/v1",      // metadata name: always denied
		"http://100.100.100.200/latest/meta-data/", // vendor metadata: always denied
	} {
		if _, ok := ProviderPolicyForBaseURL(u); ok {
			t.Errorf("unlisted private/metadata destination must be denied: %q", u)
		}
	}
}

func TestTrustedProviderHostsAcceptExplicitLocalRelayButNeverMetadata(t *testing.T) {
	resetProviderTrustState(t)
	SetTrustedProviderHosts([]string{
		"127.0.0.1:8899", "10.0.0.5", "169.254.169.254", "metadata.google.internal",
	})
	got := TrustedProviderHosts()
	for _, want := range []string{"127.0.0.1:8899", "10.0.0.5"} {
		if !hasHost(got, want) {
			t.Errorf("explicit local relay %q missing from trusted hosts: %v", want, got)
		}
	}
	for _, denied := range []string{"169.254.169.254", "metadata.google.internal"} {
		if hasHost(got, denied) {
			t.Errorf("metadata destination %q must never enter the trusted set: %v", denied, got)
		}
	}
}

func TestSeedTrustedProviderHostsAcceptsExplicitLocalRelayButNeverMetadata(t *testing.T) {
	resetProviderTrustState(t)
	SeedTrustedProviderHosts("127.0.0.1:8899", "10.0.0.5", "169.254.169.254")
	got := TrustedProviderHosts()
	if !hasHost(got, "127.0.0.1:8899") || !hasHost(got, "10.0.0.5") {
		t.Fatalf("seeded local/private relay missing from trusted hosts: %v", got)
	}
	if hasHost(got, "169.254.169.254") {
		t.Fatalf("metadata destination must never be seeded: %v", got)
	}
}

func TestTrustedProviderHostsFromEnvKeepsLocalRelayDropsMetadata(t *testing.T) {
	resetProviderTrustState(t)
	t.Setenv("ARTEX_TRUSTED_PROVIDER_HOSTS", "127.0.0.1:8899,10.0.0.5,169.254.169.254,metadata.google.internal,api.example.com")
	t.Setenv("ARTEX_LLM_BASE_URL", "")
	got := TrustedProviderHostsFromEnv()
	for _, want := range []string{"127.0.0.1:8899", "10.0.0.5", "api.example.com"} {
		found := false
		for _, h := range got {
			if strings.EqualFold(h, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("explicit operator entry %q missing from env trusted hosts: %v", want, got)
		}
	}
	for _, denied := range []string{"169.254.169.254", "metadata.google.internal"} {
		for _, h := range got {
			if strings.EqualFold(h, denied) {
				t.Errorf("metadata destination %q must never enter env trusted hosts: %v", denied, got)
			}
		}
	}
}

func TestTrustedProviderHostsFromEnvDropsPrivateBaseURLFallback(t *testing.T) {
	resetProviderTrustState(t)
	t.Setenv("ARTEX_TRUSTED_PROVIDER_HOSTS", "")
	for _, baseURL := range []string{
		"http://127.0.0.1:8899/v1",
		"http://169.254.169.254/latest/meta-data/",
	} {
		t.Setenv("ARTEX_LLM_BASE_URL", baseURL)
		if got := TrustedProviderHostsFromEnv(); len(got) != 0 {
			t.Fatalf("private/metadata fallback was trusted from %q: %v", baseURL, got)
		}
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

func TestSetTrustedProviderPolicyRejectsMetadata(t *testing.T) {
	resetProviderTrustState(t)
	SetTrustedProviderPolicy(ProviderEgressPolicy{
		ExpectedHost: "169.254.169.254", ExpectedPort: "80", ExpectedScheme: "http",
	}, true)
	if _, ok := ProviderPolicyForBaseURL("http://169.254.169.254/latest/meta-data/"); ok {
		t.Fatal("a metadata destination must never be authorized by an explicit policy")
	}
}
