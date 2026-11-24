package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// VIII.4.6 Gate B/G — provider transport closure: redirects, reuse, TLS
// hostname/SNI, and per-identity isolation. Local fixtures only.

func resetTrustedProviderPolicy(t *testing.T) {
	t.Helper()
	SetTrustedProviderPolicy(ProviderEgressPolicy{}, false)
	t.Cleanup(func() { SetTrustedProviderPolicy(ProviderEgressPolicy{}, false) })
}

func policyForServer(t *testing.T, rawURL string) ProviderEgressPolicy {
	t.Helper()
	host, port, err := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(rawURL, "http://"), "https://"))
	if err != nil {
		t.Fatalf("split %q: %v", rawURL, err)
	}
	scheme := "http"
	if strings.HasPrefix(rawURL, "https://") {
		scheme = "https"
	}
	return ProviderEgressPolicy{ExpectedHost: host, ExpectedPort: port, ExpectedScheme: scheme}
}

func TestProviderClient_RejectsRedirects(t *testing.T) {
	resetTrustedProviderPolicy(t)
	var targetHits int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetHits, 1)
		_, _ = w.Write([]byte("target"))
	}))
	defer target.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()

	pol := policyForServer(t, srv.URL)
	SetTrustedProviderPolicy(pol, true)
	client, err := quotaAwareHTTPClient("", "", srv.URL)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	_, err = client.Get(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "redirects are not allowed") {
		t.Fatalf("provider redirect must be rejected, got err=%v", err)
	}
	if n := atomic.LoadInt32(&targetHits); n != 0 {
		t.Fatalf("redirect target was contacted %d times", n)
	}
}

func TestProviderClient_NoConnectionReuse(t *testing.T) {
	resetTrustedProviderPolicy(t)
	var newConns int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			atomic.AddInt32(&newConns, 1)
		}
	}
	srv.Start()
	defer srv.Close()

	pol := policyForServer(t, srv.URL)
	SetTrustedProviderPolicy(pol, true)
	client, err := quotaAwareHTTPClient("", "", srv.URL)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	for i := 0; i < 2; i++ {
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if n := atomic.LoadInt32(&newConns); n != 2 {
		t.Fatalf("expected one fresh connection per request (DisableKeepAlives), got %d", n)
	}
}

// makeServerCert returns a self-signed certificate/key valid for name and a
// pool that trusts it.
func makeServerCert(t *testing.T, name string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	return cert, pool
}

func pinnedTLSClient(policy ProviderEgressPolicy, answers map[string][]net.IPAddr, roots *x509.CertPool) *http.Client {
	dial := providerPinnedDialWithResolver(policy, (&net.Dialer{Timeout: 5 * time.Second}).DialContext, resolverFor(answers, nil))
	return &http.Client{
		Transport: &http.Transport{
			DialContext:       dial,
			TLSClientConfig:   &tls.Config{RootCAs: roots},
			DisableKeepAlives: true,
			ForceAttemptHTTP2: false,
		},
		Timeout: 10 * time.Second,
	}
}

func TestProviderTLS_SNIAndCertificateVerification(t *testing.T) {
	cert, pool := makeServerCert(t, "trusted.example")
	var mu sync.Mutex
	var seenSNI []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.TLS = &tls.Config{GetCertificate: func(hi *tls.ClientHelloInfo) (*tls.Certificate, error) {
		mu.Lock()
		seenSNI = append(seenSNI, hi.ServerName)
		mu.Unlock()
		return &cert, nil
	}}
	srv.StartTLS()
	defer srv.Close()

	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	policy := ProviderEgressPolicy{ExpectedHost: "trusted.example", ExpectedPort: port, ExpectedScheme: "https"}
	answers := map[string][]net.IPAddr{"trusted.example": ips("127.0.0.1")}
	client := pinnedTLSClient(policy, answers, pool)

	resp, err := client.Get("https://trusted.example:" + port + "/")
	if err != nil {
		t.Fatalf("matching-certificate TLS request failed: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	mu.Lock()
	snis := append([]string(nil), seenSNI...)
	mu.Unlock()
	if len(snis) == 0 || snis[0] != "trusted.example" {
		t.Fatalf("TLS SNI = %v, want trusted.example (hostname must survive pinning)", snis)
	}

	// A certificate for a different name must fail verification even though the
	// dial is pinned to the same authorized IP.
	wrongCert, wrongPool := makeServerCert(t, "other.example")
	srv2 := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv2.TLS = &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return &wrongCert, nil }}
	srv2.StartTLS()
	defer srv2.Close()
	_, port2, _ := net.SplitHostPort(srv2.Listener.Addr().String())
	policy2 := ProviderEgressPolicy{ExpectedHost: "trusted.example", ExpectedPort: port2, ExpectedScheme: "https"}
	client2 := pinnedTLSClient(policy2, answers, wrongPool)
	if _, err := client2.Get("https://trusted.example:" + port2 + "/"); err == nil {
		t.Fatal("hostname-mismatched certificate must fail verification")
	}
}

func TestProviderClient_DistinctPoliciesCannotCrossDial(t *testing.T) {
	var hitsA, hitsB int32
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitsA, 1)
		_, _ = w.Write([]byte("A"))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitsB, 1)
		_, _ = w.Write([]byte("B"))
	}))
	defer srvB.Close()

	polA := policyForServer(t, srvA.URL)
	polB := policyForServer(t, srvB.URL)
	clientA, err := quotaAwareHTTPClientWithPolicy("", "", srvA.URL, polA)
	if err != nil {
		t.Fatalf("client A: %v", err)
	}
	clientB, err := quotaAwareHTTPClientWithPolicy("", "", srvB.URL, polB)
	if err != nil {
		t.Fatalf("client B: %v", err)
	}

	// Each client reaches only its own authorized endpoint.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			resp, err := clientA.Get(srvA.URL)
			if err != nil {
				t.Errorf("A request: %v", err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
		go func() {
			defer wg.Done()
			resp, err := clientB.Get(srvB.URL)
			if err != nil {
				t.Errorf("B request: %v", err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()

	// Cross-policy requests must be denied before any dial.
	if _, err := clientA.Get(srvB.URL); err == nil {
		t.Fatal("client A was allowed to reach server B (cross-policy dial)")
	}
	if _, err := clientB.Get(srvA.URL); err == nil {
		t.Fatal("client B was allowed to reach server A (cross-policy dial)")
	}
	if atomic.LoadInt32(&hitsA) == 0 || atomic.LoadInt32(&hitsB) == 0 {
		t.Fatalf("expected both authorized endpoints to be reached: A=%d B=%d", hitsA, hitsB)
	}
}

func TestProviderClient_ProxyModeFailsClosed(t *testing.T) {
	resetTrustedProviderPolicy(t)
	_, err := quotaAwareHTTPClient("http://127.0.0.1:3128", "", "https://trusted.example/v1")
	if err == nil || !strings.Contains(err.Error(), "incompatible with pinned provider egress") {
		t.Fatalf("provider proxy mode must fail closed at construction, got err=%v", err)
	}
}
