package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// VIII.2.2 — connection-level accounting. Replaces dialFn with a counting
// dialer that delegates to the real one, then asserts authorized requests
// dial and unauthorized requests never reach the dial layer.
func withDialCounter(t *testing.T) *int32 {
	t.Helper()
	var dials int32
	prev := dialFn
	dialFn = func(ctx context.Context, network, addr string) (net.Conn, error) {
		atomic.AddInt32(&dials, 1)
		return defaultDialContext(ctx, network, addr)
	}
	t.Cleanup(func() { dialFn = prev })
	return &dials
}

func TestNetworkBoundaryAccounting(t *testing.T) {
	dials := withDialCounter(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Value: "127.0.0.1"}})
	defer ClearTaskScope()

	// A. authorized local destination: dials happen, request succeeds.
	before := atomic.LoadInt32(dials)
	res, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second})
	if err != nil || res.Status != 200 {
		t.Fatalf("authorized request failed: err=%v res=%+v", err, res)
	}
	afterAuth := atomic.LoadInt32(dials)
	if afterAuth <= before {
		t.Fatalf("authorized request made no dial (before=%d after=%d)", before, afterAuth)
	}

	// B. unauthorized destination: rejected with ZERO new dials.
	before = atomic.LoadInt32(dials)
	_, err = doHTTP(context.Background(), httpCall{Method: "GET", URL: "http://10.255.255.1/", NoJar: true, Timeout: 5 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("unauthorized request should be rejected with scope error, got %v", err)
	}
	if got := atomic.LoadInt32(dials) - before; got != 0 {
		t.Fatalf("unauthorized request attempted %d connection(s) — must be 0", got)
	}

	// C. invalid scope entry: rejected, zero dials.
	before = atomic.LoadInt32(dials)
	SetTaskScope([]db.TaskScope{{Kind: "ip", Value: "not-an-ip"}})
	_, err = doHTTP(context.Background(), httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("invalid scope should reject, got %v", err)
	}
	if got := atomic.LoadInt32(dials) - before; got != 0 {
		t.Fatalf("invalid scope attempted %d connection(s)", got)
	}

	// D. repeated: an earlier authorization must not authorize a new destination.
	SetTaskScope([]db.TaskScope{{Kind: "ip", Value: "127.0.0.1"}})
	if _, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	before = atomic.LoadInt32(dials)
	if _, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: "http://10.255.255.1/", NoJar: true, Timeout: 5 * time.Second}); err == nil {
		t.Fatal("second destination must be reauthorized")
	}
	if got := atomic.LoadInt32(dials) - before; got != 0 {
		t.Fatalf("repeat test: %d unauthorized dials", got)
	}
}

// Redirect rejection must not dial the destination host.
func TestRedirectNoDial(t *testing.T) {
	dials := withDialCounter(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/r" {
			w.Header().Set("Location", "http://localhost:1/out") // out of scope
			w.WriteHeader(302)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Value: "127.0.0.1"}})
	defer ClearTaskScope()

	before := atomic.LoadInt32(dials)
	_, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: srv.URL + "/r", FollowRedirects: true, NoJar: true, Timeout: 5 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("redirect must be rejected with scope error, got %v", err)
	}
	// One dial for the initial request; the rejected redirect must add zero.
	if got := atomic.LoadInt32(dials) - before; got != 1 {
		t.Fatalf("redirect: expected exactly 1 dial (initial), got %d", got)
	}
}
