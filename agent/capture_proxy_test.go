package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func resetCaptureProxy(t *testing.T) {
	t.Helper()
	oldP, oldR := captureProxyConfig()
	t.Cleanup(func() {
		captureProxyMu.Lock()
		captureProxy, captureRoots = oldP, oldR
		captureProxyMu.Unlock()
	})
	if err := SetCaptureProxy("", ""); err != nil {
		t.Fatalf("clear capture proxy: %v", err)
	}
}

func TestCaptureProxyRejectsNonLoopback(t *testing.T) {
	resetCaptureProxy(t)
	for _, addr := range []string{"http://example.com:8080", "socks5://127.0.0.1:1080", "not a url"} {
		if err := SetCaptureProxy(addr, ""); err == nil {
			t.Fatalf("SetCaptureProxy(%q) should be rejected", addr)
		}
	}
	if err := SetCaptureProxy("http://127.0.0.1:8080", ""); err != nil {
		t.Fatalf("loopback proxy rejected: %v", err)
	}
}

func TestCaptureProxyRoutesHTTPRequest(t *testing.T) {
	resetCaptureProxy(t)

	// Recording proxy: Go's transport sends absolute-form requests to it.
	var proxied []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = append(proxied, r.URL.String())
		fmt.Fprint(w, "proxied-ok")
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	if err := SetCaptureProxy(proxy.URL, ""); err != nil {
		t.Fatalf("SetCaptureProxy: %v", err)
	}

	// Logical target is in scope; it never has to be reachable because the
	// request terminates at the recording proxy.
	ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Value: "10.10.10.10"}})
	defer ClearTaskScope()

	res, err := doHTTP(context.Background(), httpCall{
		Method: "GET", URL: "http://10.10.10.10:8080/probe", NoJar: true, Timeout: 5 * time.Second,
	})
	if err != nil || res.Status != 200 {
		t.Fatalf("proxied request failed: err=%v res=%+v", err, res)
	}
	if len(proxied) != 1 || !strings.Contains(proxied[0], "10.10.10.10:8080/probe") {
		t.Fatalf("request did not reach recording proxy: %v", proxied)
	}
	if got := string(res.Body); got != "proxied-ok" {
		t.Fatalf("body=%q", got)
	}
	_ = pu
}

func TestCaptureProxyStillEnforcesScopeBeforeDial(t *testing.T) {
	resetCaptureProxy(t)
	if err := SetCaptureProxy("http://127.0.0.1:18081", ""); err != nil {
		t.Fatalf("SetCaptureProxy: %v", err)
	}

	dials := withDialCounter(t)
	ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Value: "127.0.0.1"}})
	defer ClearTaskScope()

	before := *dials
	_, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: "http://10.255.255.1/", NoJar: true, Timeout: 5 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("out-of-scope request should be denied with proxy configured, got %v", err)
	}
	if got := *dials - before; got != 0 {
		t.Fatalf("out-of-scope request made %d dial(s) even with proxy configured", got)
	}
}

func TestCaptureProxyPinsDialToProxy(t *testing.T) {
	resetCaptureProxy(t)
	proxy, _ := url.Parse("http://127.0.0.1:18080")
	dial := captureProxyDial(proxy)
	if _, err := dial(context.Background(), "tcp", "127.0.0.1:9999"); err == nil {
		t.Fatal("dial to a non-proxy address must be denied")
	}
	// A listener on the configured proxy address is reachable.
	ln, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		t.Skipf("port unavailable: %v", err)
	}
	defer ln.Close()
	conn, err := dial(context.Background(), "tcp", "127.0.0.1:18080")
	if err != nil {
		t.Fatalf("proxy dial failed: %v", err)
	}
	_ = conn.Close()
}
