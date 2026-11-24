package traffic

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

// VIII.4.6 Gate D — traffic-proxy upstream destination enforcement.
//
// go-mitmproxy v1.9.2 owns the upstream dial (getUpstreamConn) and exposes no
// dialer hook. The only supported extension point is SetUpstreamProxy, whose
// return value is dialed as an HTTP proxy. ARTEX therefore routes every sealed
// upstream connection through this loopback-only forwarder: the forwarder
// re-checks the allowlist, resolves the destination itself, rejects mixed or
// unauthorized answers, and dials the exact validated IP. The original
// hostname is carried by the CONNECT authority, so the client's TLS SNI and
// certificate verification are untouched.

// egressDialer validates a destination and pins the TCP dial to the validated
// address. Every field is injected so tests never touch global state.
type egressDialer struct {
	allowed  func(host, port string) bool
	prefixes func() []netip.Prefix
	resolve  func(context.Context, string) ([]net.IPAddr, error)
	dial     func(context.Context, string, string) (net.Conn, error)
}

func (d *egressDialer) prefixList() []netip.Prefix {
	if d.prefixes == nil {
		return nil
	}
	return d.prefixes()
}

func (d *egressDialer) dialPinned(ctx context.Context, host, port string) (net.Conn, error) {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" || port == "" {
		return nil, fmt.Errorf("traffic egress: incomplete destination %q (DENY)", net.JoinHostPort(host, port))
	}
	if d.allowed != nil && !d.allowed(host, port) {
		return nil, fmt.Errorf("traffic egress: destination %s (DENY: not in allowlist)", net.JoinHostPort(host, port))
	}
	dial := d.dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: 15 * time.Second}).DialContext
	}
	if ip := net.ParseIP(host); ip != nil {
		if !trafficPrefixAllowed(ip, d.prefixList()) {
			return nil, fmt.Errorf("traffic egress: literal %s outside allowed prefixes (DENY)", ip)
		}
		return dial(ctx, "tcp", net.JoinHostPort(ip.String(), port))
	}
	resolve := d.resolve
	if resolve == nil {
		resolve = func(ctx context.Context, h string) ([]net.IPAddr, error) {
			return net.DefaultResolver.LookupIPAddr(ctx, h)
		}
	}
	addrs, err := resolve(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("traffic egress: DNS resolution failed for %q (DENY)", host)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("traffic egress: empty DNS answer for %q (DENY)", host)
	}
	var selected net.IP
	for _, a := range addrs {
		if a.IP == nil {
			return nil, fmt.Errorf("traffic egress: malformed DNS answer for %q (DENY)", host)
		}
		if !trafficPrefixAllowed(a.IP, d.prefixList()) {
			return nil, fmt.Errorf("traffic egress: DNS answer %s for %q is not authorized (DENY: mixed answer set)", a.IP, host)
		}
		if selected == nil {
			selected = a.IP
		}
	}
	return dial(ctx, "tcp", net.JoinHostPort(selected.String(), port))
}

func trafficPrefixAllowed(ip net.IP, prefixes []netip.Prefix) bool {
	if len(prefixes) == 0 {
		return true
	}
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// egressForwarder is a loopback-only CONNECT tunnel that performs the pinned
// upstream dial on behalf of go-mitmproxy.
type egressForwarder struct {
	ln     net.Listener
	dial   *egressDialer
	closed chan struct{}
	once   sync.Once
}

func newEgressForwarder(d *egressDialer) (*egressForwarder, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("traffic egress: forwarder listen: %w", err)
	}
	f := &egressForwarder{ln: ln, dial: d, closed: make(chan struct{})}
	go f.serve()
	return f, nil
}

func (f *egressForwarder) url() *url.URL {
	return &url.URL{Scheme: "http", Host: f.ln.Addr().String()}
}

func (f *egressForwarder) close() error {
	f.once.Do(func() {
		close(f.closed)
		_ = f.ln.Close()
	})
	return nil
}

func (f *egressForwarder) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			select {
			case <-f.closed:
				return
			default:
				return
			}
		}
		go f.handle(c)
	}
}

func (f *egressForwarder) handle(c net.Conn) {
	defer c.Close()
	// Bound the request-read phase; cleared once the tunnel is established.
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	if req.Method != http.MethodConnect {
		writeForwarderStatus(c, http.StatusMethodNotAllowed)
		return
	}
	dest := strings.TrimSpace(req.Host)
	if dest == "" && req.URL != nil {
		dest = strings.TrimSpace(req.URL.Host)
	}
	host, port := egressSplitHostPort(dest)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	up, err := f.dial.dialPinned(ctx, host, port)
	cancel()
	if err != nil {
		writeForwarderStatus(c, http.StatusBadGateway)
		return
	}
	defer up.Close()
	_ = c.SetDeadline(time.Time{})
	if _, err := io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, br); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, up); done <- struct{}{} }()
	<-done
}

func writeForwarderStatus(c net.Conn, code int) {
	text := http.StatusText(code)
	if text == "" {
		text = "Denied"
	}
	_, _ = fmt.Fprintf(c, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", code, text)
}

func egressSplitHostPort(addr string) (string, string) {
	if h, p, err := net.SplitHostPort(addr); err == nil {
		return h, p
	}
	return addr, ""
}
