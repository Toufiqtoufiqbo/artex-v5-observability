package traffic

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// VIII.4.6 Gate D — traffic-proxy upstream destination enforcement.
// All fixtures are loopback-only; no external DNS or network is used.

func startEchoServer(t *testing.T) (string, *int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var accepts int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&accepts, 1)
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String(), &accepts
}

func splitAddr(t *testing.T, addr string) (string, string) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	return h, p
}

func startForwarder(t *testing.T, d *egressDialer) *egressForwarder {
	t.Helper()
	f, err := newEgressForwarder(d)
	if err != nil {
		t.Fatalf("forwarder: %v", err)
	}
	t.Cleanup(func() { _ = f.close() })
	return f
}

// connectThrough sends a CONNECT to the forwarder and returns the status line
// and the tunneled connection (nil when the forwarder denied the request).
func connectThrough(t *testing.T, f *egressForwarder, authority string) (string, net.Conn) {
	t.Helper()
	c, err := net.Dial("tcp", f.ln.Addr().String())
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", authority, authority)
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		c.Close()
		t.Fatalf("read status: %v", err)
	}
	// Drain the rest of the response headers.
	for {
		l, err := br.ReadString('\n')
		if err != nil || strings.TrimSpace(l) == "" {
			break
		}
	}
	if strings.Contains(line, "200") {
		return line, &bufferedConn{Conn: c, r: br}
	}
	c.Close()
	return line, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

func TestEgressForwarder_AuthorizedDestinationPinned(t *testing.T) {
	echoAddr, _ := startEchoServer(t)
	_, echoPort := splitAddr(t, echoAddr)

	var dialed []string
	f := startForwarder(t, &egressDialer{
		allowed: func(h, p string) bool { return h == "fixture.example" && p == echoPort },
		resolve: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		},
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	})
	line, conn := connectThrough(t, f, "fixture.example:"+echoPort)
	if conn == nil || !strings.Contains(line, "200") {
		t.Fatalf("authorized CONNECT denied: %q", line)
	}
	defer conn.Close()
	if len(dialed) != 1 || dialed[0] != "127.0.0.1:"+echoPort {
		t.Fatalf("upstream dial = %v, want pinned 127.0.0.1:%s", dialed, echoPort)
	}
	// The tunnel actually reaches the fixture.
	msg := []byte("ping-through-tunnel")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write tunnel: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read tunnel: %v", err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("tunnel echo = %q", buf)
	}
}

func TestEgressForwarder_UnlistedHostAndPortDeniedZeroDials(t *testing.T) {
	echoAddr, _ := startEchoServer(t)
	_, echoPort := splitAddr(t, echoAddr)

	var dials int32
	f := startForwarder(t, &egressDialer{
		allowed: func(h, p string) bool { return h == "fixture.example" && p == echoPort },
		resolve: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		},
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			atomic.AddInt32(&dials, 1)
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	})
	for _, authority := range []string{
		"evil.example:" + echoPort, // unlisted host
		"fixture.example:1",        // unlisted port
	} {
		line, conn := connectThrough(t, f, authority)
		if conn != nil || !strings.Contains(line, "502") {
			t.Errorf("CONNECT %s must be denied, got %q", authority, line)
		}
	}
	if n := atomic.LoadInt32(&dials); n != 0 {
		t.Fatalf("denied destinations reached the dialer %d times", n)
	}
}

func TestEgressForwarder_MixedAnswersDenied(t *testing.T) {
	for _, answers := range [][]net.IPAddr{
		{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("10.0.0.1")}},
		{{IP: net.ParseIP("10.0.0.1")}, {IP: net.ParseIP("127.0.0.1")}},
	} {
		var dials int32
		f := startForwarder(t, &egressDialer{
			allowed:  func(string, string) bool { return true },
			prefixes: func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")} },
			resolve: func(context.Context, string) ([]net.IPAddr, error) {
				return answers, nil
			},
			dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
				atomic.AddInt32(&dials, 1)
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		})
		line, conn := connectThrough(t, f, "fixture.example:443")
		if conn != nil || !strings.Contains(line, "502") {
			t.Fatalf("mixed answer set %v must be denied, got %q", answers, line)
		}
		if n := atomic.LoadInt32(&dials); n != 0 {
			t.Fatalf("mixed answer set reached the dialer %d times", n)
		}
	}
}

func TestEgressForwarder_ResolverFailuresZeroDials(t *testing.T) {
	cases := map[string]func(context.Context, string) ([]net.IPAddr, error){
		"error":     func(context.Context, string) ([]net.IPAddr, error) { return nil, errors.New("timeout") },
		"empty":     func(context.Context, string) ([]net.IPAddr, error) { return nil, nil },
		"malformed": func(context.Context, string) ([]net.IPAddr, error) { return []net.IPAddr{{IP: nil}}, nil },
		"cancel":    func(ctx context.Context, _ string) ([]net.IPAddr, error) { return nil, ctx.Err() },
	}
	for name, resolve := range cases {
		t.Run(name, func(t *testing.T) {
			var dials int32
			f := startForwarder(t, &egressDialer{
				allowed: func(string, string) bool { return true },
				resolve: resolve,
				dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
					atomic.AddInt32(&dials, 1)
					return (&net.Dialer{}).DialContext(ctx, network, addr)
				},
			})
			line, conn := connectThrough(t, f, "fixture.example:443")
			if conn != nil || !strings.Contains(line, "502") {
				t.Fatalf("resolver failure %q must be denied, got %q", name, line)
			}
			if n := atomic.LoadInt32(&dials); n != 0 {
				t.Fatalf("resolver failure reached the dialer %d times", n)
			}
		})
	}
}

func TestEgressForwarder_RebindingRevalidatedPerConnection(t *testing.T) {
	echoAddr, _ := startEchoServer(t)
	_, echoPort := splitAddr(t, echoAddr)

	call := 0
	var dials int32
	f := startForwarder(t, &egressDialer{
		allowed:  func(string, string) bool { return true },
		prefixes: func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")} },
		resolve: func(context.Context, string) ([]net.IPAddr, error) {
			call++
			if call == 1 {
				return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
			}
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil // rebound
		},
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			atomic.AddInt32(&dials, 1)
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	})
	line, conn := connectThrough(t, f, "fixture.example:"+echoPort)
	if conn == nil {
		t.Fatalf("first connection must be allowed: %q", line)
	}
	conn.Close()
	line, conn = connectThrough(t, f, "fixture.example:"+echoPort)
	if conn != nil || !strings.Contains(line, "502") {
		t.Fatalf("rebound answer must be denied, got %q", line)
	}
	if n := atomic.LoadInt32(&dials); n != 1 {
		t.Fatalf("dialer calls = %d, want 1 (rebound answer must not dial)", n)
	}
}

func TestEgressForwarder_ListenerIsLoopbackOnly(t *testing.T) {
	f := startForwarder(t, &egressDialer{allowed: func(string, string) bool { return false }})
	host, _, err := net.SplitHostPort(f.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("forwarder bound to %q, want 127.0.0.1", host)
	}
}

func TestTraffic_SealedPolicyAndChainedProxy(t *testing.T) {
	echoAddr, _ := startEchoServer(t)
	_, echoPort := splitAddr(t, echoAddr)
	f := startForwarder(t, &egressDialer{
		allowed:  func(string, string) bool { return true },
		resolve:  func(context.Context, string) ([]net.IPAddr, error) { return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil },
	})
	tr := &Traffic{forwarder: f}
	tr.SetEgressAllowlist([]string{"fixture.example:" + echoPort}, true)

	req, _ := http.NewRequest("GET", "http://fixture.example:"+echoPort+"/", nil)
	u, err := tr.upstreamProxyFor(req)
	if err != nil || u == nil || u.Host != f.ln.Addr().String() {
		t.Fatalf("sealed allowlisted destination must use the pinned forwarder: u=%v err=%v", u, err)
	}

	// Unlisted destination → explicit denial.
	bad, _ := http.NewRequest("GET", "http://evil.example/", nil)
	if _, err := tr.upstreamProxyFor(bad); err == nil {
		t.Fatal("unlisted destination must be denied")
	}

	// A chained upstream proxy cannot be pinned → fail closed.
	if err := tr.SetUpstreamProxy("socks5://127.0.0.1:1080"); err != nil {
		t.Fatalf("set upstream: %v", err)
	}
	if _, err := tr.upstreamProxyFor(req); err == nil || !strings.Contains(err.Error(), "incompatible with sealed pinned egress") {
		t.Fatalf("chained upstream under sealed policy must fail closed, got %v", err)
	}
}
