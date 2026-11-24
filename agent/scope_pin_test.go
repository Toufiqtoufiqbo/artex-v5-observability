package agent

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// VIII.4.5 — destination integrity: address validation + pinned dial.

func TestAddressAuthorized_IPEntry(t *testing.T) {
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(101, []db.TaskScope{{Kind: "ip", Net: "127.0.0.2/32"}})
	key := scopeKeyForTask(101)
	if !addressAuthorized(key, net.ParseIP("127.0.0.2"), "127.0.0.2") {
		t.Fatal("127.0.0.2 must be authorized")
	}
	if addressAuthorized(key, net.ParseIP("127.0.0.3"), "127.0.0.3") {
		t.Fatal("127.0.0.3 must NOT be authorized")
	}
}

func TestAddressAuthorized_RootDomain(t *testing.T) {
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(101, []db.TaskScope{{Kind: "root_domain", Domain: "example.com"}})
	key := scopeKeyForTask(101)
	if !addressAuthorized(key, net.ParseIP("1.2.3.4"), "www.example.com") {
		t.Fatal("subdomain of root_domain must authorize the address")
	}
	if addressAuthorized(key, net.ParseIP("9.9.9.9"), "other.net") {
		t.Fatal("unlisted host must not authorize")
	}
}

func TestDoHTTP_DialsAuthorizedIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(101, []db.TaskScope{{Kind: "ip", Net: hostOf(srv.URL) + "/32"}})
	ctx := WithRunInfo(context.Background(), RunInfo{TaskID: 101})
	res, err := doHTTP(ctx, httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second})
	if err != nil || res.Status != 200 {
		t.Fatalf("authorized pinned dial failed: %v", err)
	}
}

func TestDoHTTP_LiteralUnauthorizedDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(101, []db.TaskScope{{Kind: "ip", Net: hostOf(srv.URL) + "/32"}})
	// Request to a DIFFERENT literal than the authorized one → scopeGuard denies first.
	_, err := doHTTP(context.Background(), httpCall{Method: "GET", URL: "http://10.255.255.1/", NoJar: true, Timeout: 2 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("expected scope denial, got %v", err)
	}
}
