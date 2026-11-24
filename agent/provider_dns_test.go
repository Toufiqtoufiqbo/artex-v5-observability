package agent

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

// VIII.4.6 Gate A/G — provider DNS-to-dial with a deterministic,
// request-scoped resolver. No global resolver or pin state is used.

func ips(ss ...string) []net.IPAddr {
	out := make([]net.IPAddr, 0, len(ss))
	for _, s := range ss {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	return out
}

func resolverFor(m map[string][]net.IPAddr, errFor map[string]error) func(context.Context, string) ([]net.IPAddr, error) {
	return func(_ context.Context, host string) ([]net.IPAddr, error) {
		if err := errFor[host]; err != nil {
			return nil, err
		}
		return m[host], nil
	}
}

func fakeBaseDial(dials *[]string) func(context.Context, string, string) (net.Conn, error) {
	return func(_ context.Context, _, addr string) (net.Conn, error) {
		*dials = append(*dials, addr)
		return nil, errors.New("fake dial")
	}
}

func trustedPolicy() ProviderEgressPolicy {
	return ProviderEgressPolicy{ExpectedHost: "trusted.example", ExpectedPort: "443", ExpectedScheme: "https"}
}

// resetProviderTrustedHosts restores the package-level trusted-host state so a
// test cannot leak its operator host set into another test.
func resetProviderTrustedHosts(t *testing.T) {
	t.Helper()
	providerEgressMu.Lock()
	providerTrustedHosts = nil
	providerHostsSeeded = false
	providerEgressMu.Unlock()
	t.Cleanup(func() {
		providerEgressMu.Lock()
		providerTrustedHosts = nil
		providerHostsSeeded = false
		providerEgressMu.Unlock()
	})
}

func TestProviderDNS_TrustedHostnamePinsAuthorizedAnswer(t *testing.T) {
	m := map[string][]net.IPAddr{"trusted.example": ips("1.2.3.4")}
	var dials []string
	dial := providerPinnedDialWithResolver(trustedPolicy(), fakeBaseDial(&dials), resolverFor(m, nil))

	_, _ = dial(context.Background(), "tcp", "trusted.example:443")
	if len(dials) != 1 || dials[0] != "1.2.3.4:443" {
		t.Fatalf("expected pinned dial to 1.2.3.4:443, got %v", dials)
	}

	// Case normalization is the only permitted hostname equivalence.
	dials = nil
	_, _ = dial(context.Background(), "tcp", "TRUSTED.EXAMPLE:443")
	if len(dials) != 1 || dials[0] != "1.2.3.4:443" {
		t.Fatalf("case-variant of the trusted host must pin the same address, got %v", dials)
	}
}

func TestProviderDNS_DifferentHostSameIP(t *testing.T) {
	// The VIII.4.6.1 defect: an attacker-controlled hostname resolving to the
	// trusted host's IP must NOT become trusted by IP equality.
	m := map[string][]net.IPAddr{
		"trusted.example": ips("1.2.3.4"),
		"evil.example":    ips("1.2.3.4"),
	}
	var dials []string
	dial := providerPinnedDialWithResolver(trustedPolicy(), fakeBaseDial(&dials), resolverFor(m, nil))
	if _, err := dial(context.Background(), "tcp", "evil.example:443"); err == nil || len(dials) != 0 {
		t.Fatalf("same-IP different-hostname must be denied with zero dials: err=%v dials=%v", err, dials)
	}
}

func TestProviderDNS_HostnameIdentityDenials(t *testing.T) {
	m := map[string][]net.IPAddr{
		"trusted.example":        ips("1.2.3.4"),
		"sub.trusted.example":    ips("1.2.3.4"),
		"example":                ips("1.2.3.4"),
		"trusted.example.evil":   ips("1.2.3.4"),
		"trusted.example.":       ips("1.2.3.4"),
		"xtrusted.example":       ips("1.2.3.4"),
		"trusted.example.evil.":  ips("1.2.3.4"),
		"tr\u00fcsted.example":   ips("1.2.3.4"), // non-ASCII / IDNA-ish
	}
	var dials []string
	dial := providerPinnedDialWithResolver(trustedPolicy(), fakeBaseDial(&dials), resolverFor(m, nil))
	denied := []string{
		"sub.trusted.example:443",   // subdomain
		"example:443",               // parent domain
		"trusted.example.evil:443",  // suffix lookalike
		"trusted.example.:443",      // trailing dot
		"xtrusted.example:443",      // prefix lookalike
		"trusted.example.evil.:443", // suffix + dot
		"trüsted.example:443",       // unicode
		"127.0.0.1:443",             // IP literal not named by the policy
		":443",                      // empty host
	}
	for _, addr := range denied {
		before := len(dials)
		if _, err := dial(context.Background(), "tcp", addr); err == nil {
			t.Errorf("unauthorized dial address %q was allowed", addr)
		}
		if len(dials) != before {
			t.Errorf("unauthorized dial address %q reached the base dialer", addr)
		}
	}
}

func TestProviderDNS_PortBinding(t *testing.T) {
	m := map[string][]net.IPAddr{"trusted.example": ips("1.2.3.4")}
	var dials []string
	dial := providerPinnedDialWithResolver(trustedPolicy(), fakeBaseDial(&dials), resolverFor(m, nil))
	if _, err := dial(context.Background(), "tcp", "trusted.example:8443"); err == nil || len(dials) != 0 {
		t.Fatalf("wrong port must be denied with zero dials: err=%v dials=%v", err, dials)
	}
}

func TestProviderDNS_MixedAnswersFailClosedRegardlessOfOrder(t *testing.T) {
	pol := trustedPolicy()
	pol.AllowedPrefixes = []netip.Prefix{netip.MustParsePrefix("1.2.3.0/24")}
	for _, answer := range [][]net.IPAddr{
		ips("1.2.3.4", "10.0.0.1"),
		ips("10.0.0.1", "1.2.3.4"),
	} {
		m := map[string][]net.IPAddr{"trusted.example": answer}
		var dials []string
		dial := providerPinnedDialWithResolver(pol, fakeBaseDial(&dials), resolverFor(m, nil))
		if _, err := dial(context.Background(), "tcp", "trusted.example:443"); err == nil || len(dials) != 0 {
			t.Fatalf("mixed answer set %v must deny with zero dials: err=%v dials=%v", answer, err, dials)
		}
	}
	// A wholly-authorized set still dials the first permitted answer.
	m := map[string][]net.IPAddr{"trusted.example": ips("1.2.3.4", "1.2.3.5")}
	var dials []string
	dial := providerPinnedDialWithResolver(pol, fakeBaseDial(&dials), resolverFor(m, nil))
	_, _ = dial(context.Background(), "tcp", "trusted.example:443")
	if len(dials) != 1 || dials[0] != "1.2.3.4:443" {
		t.Fatalf("authorized set must pin an authorized address, got %v", dials)
	}
}

func TestProviderDNS_ResolverFailuresFailClosed(t *testing.T) {
	pol := trustedPolicy()
	m := map[string][]net.IPAddr{"trusted.example": ips("1.2.3.4")}
	errFor := map[string]error{}
	var dials []string
	dial := providerPinnedDialWithResolver(pol, fakeBaseDial(&dials), resolverFor(m, errFor))

	// Resolver error.
	errFor["trusted.example"] = errors.New("timeout")
	if _, err := dial(context.Background(), "tcp", "trusted.example:443"); err == nil || len(dials) != 0 {
		t.Fatalf("resolver timeout must fail closed: err=%v dials=%v", err, dials)
	}
	// Empty answer.
	delete(errFor, "trusted.example")
	m["trusted.example"] = nil
	if _, err := dial(context.Background(), "tcp", "trusted.example:443"); err == nil || len(dials) != 0 {
		t.Fatalf("empty answer must fail closed: err=%v dials=%v", err, dials)
	}
	// Malformed answer (nil IP).
	m["trusted.example"] = []net.IPAddr{{IP: nil}}
	if _, err := dial(context.Background(), "tcp", "trusted.example:443"); err == nil || len(dials) != 0 {
		t.Fatalf("malformed answer must fail closed: err=%v dials=%v", err, dials)
	}
	// Context cancellation.
	delete(m, "trusted.example")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolveCancelled := func(ctx context.Context, _ string) ([]net.IPAddr, error) {
		return nil, ctx.Err()
	}
	dial = providerPinnedDialWithResolver(pol, fakeBaseDial(&dials), resolveCancelled)
	if _, err := dial(ctx, "tcp", "trusted.example:443"); err == nil || len(dials) != 0 {
		t.Fatalf("cancelled context must fail closed: err=%v dials=%v", err, dials)
	}
}

func TestProviderDNS_RebindingRevalidatedPerDial(t *testing.T) {
	pol := trustedPolicy()
	pol.AllowedPrefixes = []netip.Prefix{netip.MustParsePrefix("1.2.3.0/24")}
	call := 0
	resolve := func(context.Context, string) ([]net.IPAddr, error) {
		call++
		if call == 1 {
			return ips("1.2.3.4"), nil
		}
		return ips("10.0.0.1"), nil // rebinding after the first authorized dial
	}
	var dials []string
	dial := providerPinnedDialWithResolver(pol, fakeBaseDial(&dials), resolve)

	if _, _ = dial(context.Background(), "tcp", "trusted.example:443"); len(dials) != 1 || dials[0] != "1.2.3.4:443" {
		t.Fatalf("first dial must pin the authorized answer, got %v", dials)
	}
	// The second dial must NOT reuse the stale authorization.
	if _, err := dial(context.Background(), "tcp", "trusted.example:443"); err == nil {
		t.Fatalf("rebound answer must be denied")
	}
	if len(dials) != 1 {
		t.Fatalf("rebound answer reached the dialer: %v", dials)
	}
}

func TestProviderDNS_IPLiteralTrustedPolicy(t *testing.T) {
	pol := ProviderEgressPolicy{ExpectedHost: "127.0.0.1", ExpectedPort: "8080", ExpectedScheme: "http"}
	var dials []string
	dial := providerPinnedDialWithResolver(pol, fakeBaseDial(&dials), resolverFor(nil, nil))
	// The exact literal named by the trusted policy is explicitly authorized.
	_, _ = dial(context.Background(), "tcp", "127.0.0.1:8080")
	if len(dials) != 1 || dials[0] != "127.0.0.1:8080" {
		t.Fatalf("explicitly trusted IP literal must dial unchanged, got %v", dials)
	}
	// Any other literal is denied without a dial.
	if _, err := dial(context.Background(), "tcp", "127.0.0.2:8080"); err == nil || len(dials) != 1 {
		t.Fatalf("other IP literal must be denied: dials=%v", dials)
	}
}

func TestProviderDNS_BaseURLCannotEscapeTrustedHostSet(t *testing.T) {
	resetProviderTrustedHosts(t)
	SetTrustedProviderHosts([]string{"trusted.example"})

	// Trusted host in the operator set: BaseURL-derived policy is usable.
	p, ok := providerEgressPolicyFor("https://trusted.example/v1")
	if !ok || p.ExpectedHost != "trusted.example" {
		t.Fatalf("trusted BaseURL must yield a policy: ok=%v p=%+v", ok, p)
	}
	// A runtime BaseURL naming any other host must not authorize itself.
	for _, raw := range []string{
		"https://attacker.example/v1",
		"https://sub.trusted.example/v1",
		"https://trusted.example.evil.net/v1",
		"https://trusted.example./v1",
		"https://127.0.0.1/v1",
	} {
		p, ok := providerEgressPolicyFor(raw)
		if ok {
			t.Errorf("BaseURL %q authorized itself: %+v", raw, p)
		}
		if err := CheckProviderEgress(providerReqTo(raw+"/chat"), p); err == nil {
			t.Errorf("BaseURL %q passed CheckProviderEgress", raw)
		}
	}
}
