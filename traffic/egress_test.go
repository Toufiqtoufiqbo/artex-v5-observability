package traffic

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// VIII.4.3 — traffic proxy egress allowlist: negative and positive paths.

func newFixtureOn(t *testing.T, addr, body string) *httptest.Server {
	t.Helper()
	l, err := net.Listen("tcp", addr+":0")
	if err != nil {
		t.Skipf("loopback bind %s unavailable: %v", addr, err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

// newProxyPort picks a free loopback port by probing and releasing a listener.
func newProxyPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// TestTrafficProxyEgressAllowlist — through the real proxy:
// allowed host succeeds; a host outside the allowlist is denied.
func TestTrafficProxyEgressAllowlist(t *testing.T) {
	srvA := newFixtureOn(t, "127.0.0.2", "ok-A")
	srvB := newFixtureOn(t, "127.0.0.3", "ok-B")

	tr, err := Open(filepath.Join(t.TempDir(), "traffic"), newProxyPort(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	tr.SetEgressAllowlist([]string{"127.0.0.2"}, true)
	if got := tr.AllowedEgressHosts(); len(got) != 1 || got[0] != "127.0.0.2" {
		t.Fatalf("AllowedEgressHosts = %v", got)
	}

	proxyURL, err := url.Parse(tr.ProxyAddr())
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL.Host == "" {
		t.Fatalf("ProxyAddr not usable: %q", tr.ProxyAddr())
	}

	// go-mitmproxy's Proxy.Start blocks until shutdown (same pattern as
	// server/manager.go), so it runs in a goroutine.
	go func() { _ = tr.Start() }()
	if err := waitForListener(t, proxyURL.Host, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}

	// Allowed destination (host-only entry matches req host:port): the proxy
	// must actually reach the fixture and return its body.
	res, err := client.Get("http://" + srvA.Listener.Addr().String() + "/")
	if err != nil {
		t.Fatalf("allowed egress denied: %v", err)
	}
	bodyA, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || strings.TrimSpace(string(bodyA)) != "ok-A" {
		t.Fatalf("allowed egress: status=%d body=%q", res.StatusCode, bodyA)
	}

	// Denied destination: the request must NOT reach the non-allowlisted
	// fixture — either a transport error or a non-2xx/wrong body.
	res, err = client.Get("http://" + srvB.Listener.Addr().String() + "/")
	if err == nil {
		bodyB, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 && strings.TrimSpace(string(bodyB)) == "ok-B" {
			t.Fatalf("egress to non-allowlisted host succeeded: status=%d body=%q", res.StatusCode, bodyB)
		}
	}
}

// TestTrafficProxyEgressAllowlistEmptyRestores — unsealed = legacy permissive;
// sealed with empty allowlist denies all (VIII.4.4).
func TestTrafficProxyEgressAllowlistEmptyRestores(t *testing.T) {
	tr := &Traffic{}
	tr.SetEgressAllowlist(nil, false)
	if len(tr.AllowedEgressHosts()) != 0 {
		t.Fatalf("expected empty allowlist")
	}
	req, _ := http.NewRequest("GET", "http://anything.example:443/", nil)
	if !tr.EgressAllowed(req) {
		t.Fatalf("unsealed: empty allowlist must be permissive (legacy)")
	}

	tr.SetEgressAllowlist([]string{"127.0.0.1", "example.com:8443"}, true)
	if !tr.EgressAllowed(reqTo("http://example.com:8443/x")) {
		t.Fatalf("host:port entry should match")
	}
	if !tr.EgressAllowed(reqTo("http://EXAMPLE.com:8443/x")) {
		t.Fatalf("entries are case-insensitive")
	}
	if tr.EgressAllowed(reqTo("http://example.com:9443/x")) {
		t.Fatalf("different port must be denied")
	}
	if tr.EgressAllowed(reqTo("http://sub.example.com/x")) {
		t.Fatalf("exact host entry must not authorize subdomains")
	}
	if !tr.EgressAllowed(reqTo("http://127.0.0.1:9999/x")) {
		t.Fatalf("host-only entry should match any port on that host")
	}
}

// TestTrafficEgressSealedEmptyDeniesAll — VIII.4.4: sealed, empty allowlist denies.
func TestTrafficEgressSealedEmptyDeniesAll(t *testing.T) {
	tr := &Traffic{}
	tr.SetEgressAllowlist(nil, true)
	if tr.EgressAllowed(reqTo("http://127.0.0.1:3004/")) {
		t.Fatalf("sealed-empty allowlist must deny all")
	}
	if tr.EgressAllowed(reqTo("")) {
		t.Fatalf("sealed-empty must deny empty host")
	}
}

// TestTrafficProxyAuditSink — SetEgressAuditSink receives allow/deny events.
func TestTrafficProxyEgressDefaultSealed(t *testing.T) {
	tr, err := Open(filepath.Join(t.TempDir(), "traffic"), newProxyPort(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if !tr.egressSealed.Load() {
		t.Fatalf("production Open() must default to sealed (VIII.4.4.1)")
	}
	if tr.EgressAllowed(reqTo("http://127.0.0.1:3004/")) {
		t.Fatalf("sealed with no allowlist must deny by default")
	}
}

func TestTrafficProxyAuditSink(t *testing.T) {
	type rec struct {
		host     string
		allowed  bool
		reason   string
	}
	var mu sync.Mutex
	var got []rec
	SetEgressAuditSink(func(host string, allowed bool, reason string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, rec{host: host, allowed: allowed, reason: reason})
	})
	defer SetEgressAuditSink(nil)

	tr := &Traffic{}
	req, _ := http.NewRequest("GET", "http://allowed.example:8443/x", nil)
	_ = tr.EgressAllowed(req) // no sink data yet; sink emits via the proxy hook path only
	// Direct exercise of the emitter:
	emitTrafficEgressAudit("allowed.example:8443", true, "")
	emitTrafficEgressAudit("blocked.example", false, "denied")

	mu.Lock()
	defer mu.Unlock()
	if len(got) < 2 || got[0].host != "allowed.example:8443" || !got[0].allowed || got[0].reason != "" {
		t.Fatalf("allow event: %+v", got)
	}
	found := false
	for _, g := range got {
		if g.host == "blocked.example" && !g.allowed && g.reason != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("deny event missing: %+v", got)
	}
}

func reqTo(u string) *http.Request {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		panic(err)
	}
	return req
}

// waitForListener blocks until host:port accepts a TCP connection.
func waitForListener(t *testing.T, hostport string, timeout time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", hostport, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return &timeoutError{hostport}
}

type timeoutError struct{ hostport string }

func (e *timeoutError) Error() string {
	return "listener not ready: " + e.hostport
}
