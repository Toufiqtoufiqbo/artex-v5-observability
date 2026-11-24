package agent

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// VIII.4.6 Gate C — target-HTTP boundary strengthening on the current tree:
// multi-hop redirect reauthorization and IPv6 literal dial proofs.

// TestRedirectChainRevalidatedEachHop proves that EVERY hop of a redirect
// chain is revalidated against scope, and that an unauthorized literal
// destination is never dialed.
func TestRedirectChainRevalidatedEachHop(t *testing.T) {
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Net: "127.0.0.1/32"}})

	var dials []string
	var mu sync.Mutex
	old := dialFn
	dialFn = func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dials = append(dials, addr)
		mu.Unlock()
		return old(ctx, network, addr)
	}
	t.Cleanup(func() { dialFn = old })

	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/b", http.StatusFound)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.255.255.1:1/out", http.StatusFound)
	})
	mux.HandleFunc("/out", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	call := httpCall{Method: "GET", URL: srv.URL + "/a", FollowRedirects: true, NoJar: true, Timeout: 5 * time.Second}
	if _, err := doHTTP(context.Background(), call); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("redirect chain must be rejected by scope at the out-of-scope hop, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, d := range dials {
		if strings.HasPrefix(d, "10.255.255.1") {
			t.Fatalf("unauthorized literal was dialed: %q", d)
		}
	}
}

// TestDoHTTP_IPv6LiteralPinnedDial proves a bracketed-IPv6 literal destination
// is authorized and the actual dial address is the bracketed IPv6 address.
func TestDoHTTP_IPv6LiteralPinnedDial(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback available: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	addr := ln.Addr().String() // [::1]:port
	if !strings.HasPrefix(addr, "[::1]:") {
		t.Fatalf("listener not on [::1]:port, got %q", addr)
	}
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Net: "::1/128"}})

	var mu sync.Mutex
	var dialed []string
	old := dialFn
	dialFn = func(ctx context.Context, network, a string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, a)
		mu.Unlock()
		return old(ctx, network, a)
	}
	t.Cleanup(func() { dialFn = old })

	res, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: "http://" + addr + "/", NoJar: true, Timeout: 5 * time.Second})
	if err != nil || res.Status != 200 {
		t.Fatalf("authorized IPv6 literal request failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dialed) == 0 || dialed[0] != addr {
		t.Fatalf("actual dial address = %v, want %q", dialed, addr)
	}
}

// TestDoHTTP_IPv6LiteralUnauthorizedZeroDials proves an unauthorized IPv6
// literal is rejected before any dial.
func TestDoHTTP_IPv6LiteralUnauthorizedZeroDials(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback available: %v", err)
	}
	_ = ln.Close()
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Net: "127.0.0.2/32"}})

	var dials int32
	old := dialFn
	dialFn = func(ctx context.Context, network, a string) (net.Conn, error) {
		dials++
		return old(ctx, network, a)
	}
	t.Cleanup(func() { dialFn = old })

	if _, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: "http://[::1]/", NoJar: true, Timeout: 2 * time.Second}); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("unauthorized IPv6 literal must be denied by scope, got %v", err)
	}
	if dials != 0 {
		t.Fatalf("unauthorized IPv6 literal dialed %d time(s)", dials)
	}
}
