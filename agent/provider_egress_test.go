package agent

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// VIII.4.4 — Provider Egress enforcement tests (fake transports, no real calls).

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func okTransport() http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}}, nil
	})
}

func TestProviderEgressPolicyFromConfig(t *testing.T) {
	p, _ := providerEgressPolicyFor("https://integrate.api.nvidia.com/v1")
	if p.ExpectedHost != "integrate.api.nvidia.com" || p.ExpectedPort != "443" || p.ExpectedScheme != "https" {
		t.Fatalf("policy = %+v", p)
	}
	// Empty config → fail-closed host.
	p, _ = providerEgressPolicyFor("")
	if p.ExpectedHost != "" {
		t.Fatalf("empty base URL must yield empty host: %+v", p)
	}
}

func TestProviderEgressCheckTransport(t *testing.T) {
	pol := ProviderEgressPolicy{ExpectedHost: "integrate.api.nvidia.com", ExpectedPort: "443", ExpectedScheme: "https"}
	c := &http.Client{Transport: providerEgressCheckTransport{base: okTransport(), policy: pol}}

	// Approved destination → allowed.
	req, _ := http.NewRequest("POST", "https://integrate.api.nvidia.com/v1/models", nil)
	if _, err := c.Do(req); err != nil {
		t.Fatalf("approved provider destination denied: %v", err)
	}

	denied := []string{
		"https://evil.example/",                        // unexpected host
		"http://integrate.api.nvidia.com/",             // wrong scheme
		"https://integrate.api.nvidia.com:8443/",       // unexpected port
		"https://integrate.api.nvidia.com.evil.net/",   // hostname variant
		"https://127.0.0.1/v1/",                        // IP not in policy
	}
	for _, u := range denied {
		req, _ := http.NewRequest("GET", u, nil)
		if _, err := c.Do(req); err == nil {
			t.Errorf("provider egress allowed unauthorized destination %q", u)
		}
	}
}

func TestProviderEgressCheckTransportRedirects(t *testing.T) {
	var followErr error
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "integrate.api.nvidia.com" {
			return &http.Response{
				StatusCode: 302,
				Header:     http.Header{"Location": []string{"https://evil.example/x"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}
		return nil, nil // should not be reached
	})
	pol := ProviderEgressPolicy{ExpectedHost: "integrate.api.nvidia.com", ExpectedPort: "443", ExpectedScheme: "https"}
	c := &http.Client{
		Transport:     providerEgressCheckTransport{base: inner, policy: pol},
		CheckRedirect: func(*http.Request, []*http.Request) error { return nil }, // follow
	}
	req, _ := http.NewRequest("GET", "https://integrate.api.nvidia.com/", nil)
	_, err := c.Do(req)
	followErr = err
	if followErr == nil {
		t.Fatalf("redirect to unauthorized host not denied")
	}
}

func TestProviderEgressGlobalOverride(t *testing.T) {
	defer SetTrustedProviderPolicy(ProviderEgressPolicy{}, false)
	SetTrustedProviderPolicy(ProviderEgressPolicy{ExpectedHost: "only.example", ExpectedPort: "443", ExpectedScheme: "https"}, true)
	c := &http.Client{Transport: providerEgressCheckTransport{base: okTransport(), policy: *mustPolicy()}}
	req, _ := http.NewRequest("GET", "https://only.example/", nil)
	if _, err := c.Do(req); err != nil {
		t.Fatalf("authorized override host denied: %v", err)
	}
	req, _ = http.NewRequest("GET", "https://other.example/", nil)
	if _, err := c.Do(req); err == nil {
		t.Fatal("trusted policy should deny other hosts")
	}
}

func mustPolicy() *ProviderEgressPolicy {
	p, _ := providerEgressPolicyFor("https://only.example/")
	return &p
}

func providerReqTo(u string) *http.Request {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		panic(err)
	}
	return req
}

func TestProviderEgressTrustedPolicyIndependentOfBaseURL(t *testing.T) {
	defer SetTrustedProviderPolicy(ProviderEgressPolicy{}, false)
	SetTrustedProviderPolicy(ProviderEgressPolicy{ExpectedHost: "trusted.example", ExpectedPort: "443", ExpectedScheme: "https"}, true)

	// Trusted policy must win over the BaseURL-derived policy.
	p, _ := providerEgressPolicyFor("https://attacker.example/v1")
	if p.ExpectedHost != "trusted.example" {
		t.Fatalf("trusted policy must win over BaseURL-derived: %+v", p)
	}
	c := &http.Client{Transport: providerEgressCheckTransport{base: okTransport(), policy: p}}
	if _, err := c.Do(providerReqTo("https://trusted.example/")); err != nil {
		t.Fatalf("trusted host must be allowed: %v", err)
	}
	if _, err := c.Do(providerReqTo("https://attacker.example/")); err == nil {
		t.Fatal("attacker BaseURL must be denied")
	}
}
