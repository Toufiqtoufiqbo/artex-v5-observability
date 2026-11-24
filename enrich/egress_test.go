package enrich

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// VIII.4.4 — Enrichment egress policy + TLS verification.

func TestEnrichJobEgressPolicy(t *testing.T) {
	e := New(nil, nil, 1)
	defer e.Close()

	if e.jobEgressAllowed("example.com") {
		t.Fatalf("default (unsealed) engine must deny: use SetEgressPolicy to authorize")
	}
	e.SetEgressPolicy(nil, false)
	if e.jobEgressAllowed("example.com") {
		t.Fatalf("explicitly disabled engine must deny")
	}
	e.SetEgressPolicy([]string{"127.0.0.2"}, true)
	if !e.jobEgressAllowed("127.0.0.2") {
		t.Fatalf("authorized host 127.0.0.2 must be allowed")
	}
	if e.jobEgressAllowed("127.0.0.3") {
		t.Fatalf("non-allowlisted host 127.0.0.3 must be denied")
	}
	if e.jobEgressAllowed("sub.example.com") {
		t.Fatalf("subdomain of an unlisted root must be denied")
	}
	if e.jobEgressAllowed("") {
		t.Fatalf("empty host must be denied")
	}
}

func TestEnrichDefaultDenyDropsJobsBeforeAnyNetwork(t *testing.T) {
	// VIII.4.6 Gate E1 — with no production caller of SetEgressPolicy, the
	// engine is deny-all: enqueue must drop the job (no DNS, no HTTP).
	e := New(nil, nil, 1)
	defer e.Close()
	e.ResolveDomain(1, "example.com")
	e.ProbeSite(2, "http://example.com/")
	if n := len(e.jobs); n != 0 {
		t.Fatalf("default-deny engine enqueued %d job(s); expected zero", n)
	}
}

func TestBuildClientTLSVerification(t *testing.T) {
	// VIII.4.4: enrich client has no TLS verification bypass; a self-signed
	// test CA must fail verification.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := buildClient(nil)
	_, err := c.Get(srv.URL)
	if err == nil {
		t.Fatalf("expected TLS verification failure against self-signed test server")
	}
	if !strings.Contains(err.Error(), "certificate") && !strings.Contains(err.Error(), "x509") {
		t.Fatalf("expected cert-related verification error, got: %v", err)
	}
}
