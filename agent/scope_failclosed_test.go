package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// VIII.4.1 — fail-closed target-scope authorization.
//
// These tests assert the REQUIRED behaviour. They exercise the real code path
// CheckScope → doHTTP → dialFn; no mock bypasses the guard.

func TestScopeFailClosed_Unseeded(t *testing.T) {
	ClearTaskScope()
	defer ClearTaskScope()
	err := CheckScope("http://203.0.113.10/")
	if !errors.Is(err, ErrScopeMissing) {
		t.Fatalf("unseeded: got %v, want ErrScopeMissing", err)
	}
}

func TestScopeFailClosed_Empty(t *testing.T) {
	ClearTaskScope()
	defer ClearTaskScope()
	SetTaskScope(nil)
	err := CheckScope("http://203.0.113.10/")
	if !errors.Is(err, ErrScopeEmpty) {
		t.Fatalf("empty: got %v, want ErrScopeEmpty", err)
	}
}

func TestScopeFailClosed_UnseededZeroDials(t *testing.T) {
	dials := withDialCounter(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ClearTaskScope()
	defer ClearTaskScope()
	before := atomic.LoadInt32(dials)
	_, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second})
	if err == nil {
		t.Fatalf("unseeded request succeeded")
	}
	if got := atomic.LoadInt32(dials) - before; got != 0 {
		t.Fatalf("unseeded request dialed %d time(s); must be 0", got)
	}
}

func TestScopeFailClosed_EmptyZeroDials(t *testing.T) {
	dials := withDialCounter(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ClearTaskScope()
	defer ClearTaskScope()
	SetTaskScope(nil)
	before := atomic.LoadInt32(dials)
	_, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second})
	if err == nil {
		t.Fatalf("empty-scope request succeeded")
	}
	if got := atomic.LoadInt32(dials) - before; got != 0 {
		t.Fatalf("empty-scope request dialed %d time(s); must be 0", got)
	}
}

func TestScopeFailClosed_MalformedZeroDials(t *testing.T) {
	dials := withDialCounter(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ClearTaskScope()
	defer ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Value: "garbage"}})
	before := atomic.LoadInt32(dials)
	_, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second})
	if !errors.Is(err, ErrScopeMalformed) {
		t.Fatalf("malformed scope: got %v, want ErrScopeMalformed", err)
	}
	if got := atomic.LoadInt32(dials) - before; got != 0 {
		t.Fatalf("malformed-scope request dialed %d time(s); must be 0", got)
	}
}

func TestScopeFailClosed_OutOfScope(t *testing.T) {
	ClearTaskScope()
	defer ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "root_domain", Domain: "example.com"}})
	if err := CheckScope("http://elsewhere.net/"); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("out of scope: got %v, want ErrScopeDenied", err)
	}
}

func TestScopeFailClosed_ValidScopes(t *testing.T) {
	cases := []struct {
		name   string
		scopes []db.TaskScope
		url    string
	}{
		{"root_domain", []db.TaskScope{{Kind: "root_domain", Domain: "example.com"}}, "http://sub.example.com/x"},
		{"subdomain", []db.TaskScope{{Kind: "subdomain", Domain: "api.example.com"}}, "http://api.example.com/x"},
		{"ip value", []db.TaskScope{{Kind: "ip", Value: "10.0.0.5"}}, "http://10.0.0.5/x"},
		{"ip net", []db.TaskScope{{Kind: "ip", Net: "10.0.0.5/32"}}, "http://10.0.0.5/x"},
		{"cidr", []db.TaskScope{{Kind: "cidr", Net: "10.0.0.0/24"}}, "http://10.0.0.77/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ClearTaskScope()
			defer ClearTaskScope()
			SetTaskScope(tc.scopes)
			if err := CheckScope(tc.url); err != nil {
				t.Errorf("valid scope %q: got %v, want nil", tc.name, err)
			}
		})
	}
}

func TestScopeFailClosed_InvalidEntriesNotIgnored(t *testing.T) {
	// An invalid entry must not be silently discarded while the remaining
	// valid entries authorize the request.
	ClearTaskScope()
	defer ClearTaskScope()
	SetTaskScope([]db.TaskScope{
		{Kind: "ip", Value: "10.0.0.5"},
		{Kind: "ip", Value: "garbage"},
	})
	if err := CheckScope("http://10.0.0.5/"); !errors.Is(err, ErrScopeMalformed) {
		t.Fatalf("invalid entry silently ignored: got %v, want ErrScopeMalformed", err)
	}
}

func TestScopeFailClosed_NoStaleInheritance(t *testing.T) {
	// A scope must not be inherited from an unrelated task: once SetTaskScope
	// runs again with an empty entry list the store is explicitly empty.
	ClearTaskScope()
	defer ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "root_domain", Domain: "example.com"}})
	SetTaskScope(nil)
	if err := CheckScope("http://example.com/"); !errors.Is(err, ErrScopeEmpty) {
		t.Fatalf("stale scope inherited: got %v, want ErrScopeEmpty", err)
	}
}

func TestScopeFailClosed_ValidScopeDialsAuthorized(t *testing.T) {
	dials := withDialCounter(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ClearTaskScope()
	defer ClearTaskScope()
	host := hostOf(srv.URL)
	SetTaskScope([]db.TaskScope{{Kind: "ip", Net: host + "/32"}})
	before := atomic.LoadInt32(dials)
	res, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second})
	if err != nil || res.Status != 200 {
		t.Fatalf("authorized request failed: err=%v res=%+v", err, res)
	}
	if got := atomic.LoadInt32(dials) - before; got == 0 {
		t.Fatalf("authorized request must dial at least once")
	}
}
