package traffic

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// VIII.4.7 Gate D — traffic IPv6, TLS, and pooling behavior (local fixtures
// only; no external DNS/destinations).

// --- helpers (local fixtures) ---

func newEchoClientCountingServer(t *testing.T) (string, *int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepts int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&accepts, 1)
			go func(c net.Conn) { defer c.Close(); _, _ = c.Write([]byte("ok")) }(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String(), &accepts
}

func readLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	l, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	return l
}

func probeConnectThrough(t *testing.T, f *egressForwarder, authority string) (string, net.Conn) {
	t.Helper()
	c, err := net.Dial("tcp", f.ln.Addr().String())
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = c.Write([]byte("CONNECT " + authority + " HTTP/1.1\r\nHost: " + authority + "\r\n\r\n"))
	br := bufio.NewReader(c)
	line := readLine(t, br)
	for {
		l := readLine(t, br)
		if strings.TrimSpace(l) == "" {
			break
		}
	}
	if strings.Contains(line, "200") {
		return line, &bufferedConn{Conn: c, r: br}
	}
	c.Close()
	return line, nil
}

// --- D1: IPv6 ---

func TestTrafficEgressIPv6Forms(t *testing.T) {
	t.Run("allowlist entry matches bracketed host:port authority", func(t *testing.T) {
		tr := &Traffic{}
		tr.SetEgressAllowlist([]string{"[::1]:8443"}, true)
		if !tr.EgressAllowed(reqFor(t, "http://[::1]:8443/x")) {
			t.Fatal("must allow http://[::1]:8443/x")
		}
		if tr.EgressAllowed(reqFor(t, "http://[::1]:8444/x")) {
			t.Fatal("must deny [::1]:8444 (port mismatch)")
		}
		if tr.EgressAllowed(reqFor(t, "http://[::1]/x")) {
			t.Fatal("must deny [::1] (no port vs port entry)")
		}
	})
	t.Run("bare-host entry matches IPv6 authority", func(t *testing.T) {
		tr := &Traffic{}
		tr.SetEgressAllowlist([]string{"::1"}, true)
		if !tr.EgressAllowed(reqFor(t, "http://[::1]:9000/x")) {
			t.Fatal("must allow http://[::1]:9000/x (host-only entry)")
		}
	})
	t.Run("mixed-answer IPv4+IPv6 set denied even if one member allowed", func(t *testing.T) {
		f := startForwarder(t, &egressDialer{
			allowed:  func(string, string) bool { return true },
			prefixes: func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")} },
			resolve: func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("::1")}}, nil
			},
		})
		line, conn := probeConnectThrough(t, f, "mixed.example:443")
		if conn != nil || !strings.Contains(line, "502") {
			t.Fatalf("mixed IPv4/IPv6 answer set must be denied, got %q", line)
		}
	})
	t.Run("IPv6 loopback literal through forwarder with prefix policy", func(t *testing.T) {
		ln, err := net.Listen("tcp", "[::1]:0")
		if err != nil {
			t.Skipf("no IPv6 loopback available: %v", err)
		}
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })}
		go func() { _ = srv.Serve(ln) }()
		t.Cleanup(func() { _ = srv.Close() })
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		authority := net.JoinHostPort("::1", port)
		var dialed []string
		var mu sync.Mutex
		f := startForwarder(t, &egressDialer{
			allowed:  func(h, p string) bool { return h == "::1" && p == port },
			prefixes: func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("::1/128")} },
			dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
				mu.Lock()
				dialed = append(dialed, addr)
				mu.Unlock()
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		})
		line, conn := probeConnectThrough(t, f, authority)
		if conn == nil || !strings.Contains(line, "200") {
			t.Fatalf("authorized IPv6 literal connect denied: %q", line)
		}
		defer conn.Close()
		mu.Lock()
		defer mu.Unlock()
		want := net.JoinHostPort("::1", port)
		if len(dialed) != 1 || dialed[0] != want {
			t.Fatalf("pinned IPv6 dial = %v, want [%q]", dialed, want)
		}
	})
}

// --- D2: TLS through the pinned tunnel ---

func makeTLSFixtureCert(t *testing.T, name string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	return cert, pool
}

func TestEgressForwarder_TLS_SNIAndCertThroughTunnel(t *testing.T) {
	cert, pool := makeTLSFixtureCert(t, "trusted.example")
	var mu sync.Mutex
	var seenSNI []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }))
	srv.TLS = &tls.Config{
		GetCertificate: func(hi *tls.ClientHelloInfo) (*tls.Certificate, error) {
			mu.Lock()
			seenSNI = append(seenSNI, hi.ServerName)
			mu.Unlock()
			return &cert, nil
		},
	}
	srv.StartTLS()
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	var dialed []string
	f := startForwarder(t, &egressDialer{
		allowed: func(h, p string) bool { return h == "trusted.example" && p == port },
		resolve: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		},
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	})
	line, conn := probeConnectThrough(t, f, "trusted.example:"+port)
	if conn == nil || !strings.Contains(line, "200") {
		t.Fatalf("CONNECT to TLS fixture denied: %q", line)
	}
	defer conn.Close()
	tlsConn := tls.Client(conn, &tls.Config{RootCAs: pool, ServerName: "trusted.example"})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("TLS handshake through pinned tunnel: %v", err)
	}
	defer tlsConn.Close()
	if len(dialed) != 1 || dialed[0] != "127.0.0.1:"+port {
		t.Fatalf("pinned upstream dial = %v, want [127.0.0.1:%s]", dialed, port)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seenSNI) == 0 || seenSNI[0] != "trusted.example" {
		t.Fatalf("server saw SNI %v, want [trusted.example]", seenSNI)
	}
}

// --- D3: pooling / sequential revalidation ---

func TestEgressForwarder_SequentialConnectsRevalidated(t *testing.T) {
	echoAddr, _ := newEchoClientCountingServer(t)
	_, echoPort := splitAddr(t, echoAddr)
	var dials int32
	f := startForwarder(t, &egressDialer{
		allowed: func(h, p string) bool { return h == "fixture.example" && p == echoPort },
		resolve: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		},
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dials++
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	})
	authority := "fixture.example:" + echoPort
	line1, conn1 := probeConnectThrough(t, f, authority)
	if conn1 == nil || !strings.Contains(line1, "200") {
		t.Fatalf("first connect denied: %q", line1)
	}
	conn1.Close()
	line2, conn2 := probeConnectThrough(t, f, authority)
	if conn2 == nil || !strings.Contains(line2, "200") {
		t.Fatalf("second connect denied: %q", line2)
	}
	conn2.Close()
	if dials != 2 {
		t.Fatalf("dialer calls=%d, want 2 (every CONNECT re-dials; no reuse)", dials)
	}
}

func TestEgressForwarder_DeniedHostNeverReachesFixture(t *testing.T) {
	echoAddr, accepts := newEchoClientCountingServer(t)
	_, echoPort := splitAddr(t, echoAddr)
	f := startForwarder(t, &egressDialer{
		allowed: func(h, p string) bool { return h == "fixture.example" && p == echoPort },
		resolve: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		},
	})
	// Unlisted host after an allowed session: nothing reaches the fixture.
	line, conn := probeConnectThrough(t, f, "unlisted.example:"+echoPort)
	if conn != nil || !strings.Contains(line, "502") {
		t.Fatalf("unlisted connect must be denied, got %q", line)
	}
	if n := atomic.LoadInt32(accepts); n != 0 {
		t.Fatalf("fixture accepted %d connections from a denied host", n)
	}
}
