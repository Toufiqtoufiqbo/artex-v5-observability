package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"testing"
)

// VIII.4.6.1 — provider adversarial verification (local only; no real DNS/provider).

func TestProviderAdversarial_HostnamePolicy(t *testing.T) {
	pol := ProviderEgressPolicy{ExpectedHost: "trusted.example", ExpectedPort: "443", ExpectedScheme: "https"}
	deny := []string{
		"https://evil.example/v1",
		"https://sub.trusted.example/v1",
		"https://trusted.example.evil.net/v1",
		"https://nottrusted.example/v1",
		"https://trusted.example./v1",
		"http://trusted.example/v1",
		"https://trusted.example:8443/v1",
	}
	for _, raw := range deny {
		req, _ := http.NewRequest("GET", raw, nil)
		if err := CheckProviderEgress(req, pol); err == nil {
			t.Errorf("provider policy allowed unauthorized destination %q", raw)
		}
	}
	req, _ := http.NewRequest("POST", "https://TRUSTED.EXAMPLE/v1/chat", nil)
	if err := CheckProviderEgress(req, pol); err != nil {
		t.Fatalf("case-difference of the exact trusted host must be allowed: %v", err)
	}
}

func TestProviderAdversarial_ActualDialDestination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	host, port := u.Hostname(), u.Port()

	SetTrustedProviderPolicy(ProviderEgressPolicy{ExpectedHost: host, ExpectedPort: port, ExpectedScheme: "http"}, true)
	defer SetTrustedProviderPolicy(ProviderEgressPolicy{}, false)

	client, err := quotaAwareHTTPClient("", "", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var remote string
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req = req.WithContext(httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		GotConn: func(ci httptrace.GotConnInfo) { remote = ci.Conn.RemoteAddr().String() },
	}))
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("provider request failed: %v", err)
	}
	res.Body.Close()
	if remote == "" || !strings.HasPrefix(remote, host+":") {
		t.Fatalf("actual RemoteAddr=%q; expected connection to %s", remote, host)
	}
}
