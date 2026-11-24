package agent

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// VIII.4.5.3 — adversarial: prove the dial destination matches the authorized IP.

func TestPin_DialAddressMatchesAuthorized(t *testing.T) {
	srv := newLoopbackServerOn(t, "127.0.0.2", "ok")
	var dialAddrs []string
	prev := dialFn
	dialFn = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialAddrs = append(dialAddrs, addr)
		return prev(ctx, network, addr)
	}
	t.Cleanup(func() { dialFn = prev })

	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(101, []db.TaskScope{{Kind: "ip", Net: hostOf(srv.URL) + "/32"}})
	ctx := WithRunInfo(context.Background(), RunInfo{TaskID: 101})
	res, err := doHTTP(ctx, httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second})
	if err != nil || res.Status != 200 {
		t.Fatalf("doHTTP: %v", err)
	}
	if len(dialAddrs) != 1 {
		t.Fatalf("expected 1 dial, got %d (%v)", len(dialAddrs), dialAddrs)
	}
	if !strings.Contains(dialAddrs[0], hostOf(srv.URL)) {
		t.Fatalf("dialed %q; expected destination on %s", dialAddrs[0], hostOf(srv.URL))
	}
}

func TestPin_ResolvePinnedIP_RejectsUnauthorized(t *testing.T) {
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(101, []db.TaskScope{{Kind: "ip", Net: "127.0.0.2/32"}})
	key := scopeKeyForTask(101)
	if _, err := resolvePinnedIP(context.Background(), key, "localhost"); err == nil {
		t.Fatalf("localhost (127.0.0.1, unauthorized) must be rejected")
	}
}

func TestPin_DoHTTPDeniesUnauthorizedResolved(t *testing.T) {
	srv := newLoopbackServerOn(t, "127.0.0.2", "ok")
	defer srv.Close()
	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(101, []db.TaskScope{{Kind: "ip", Net: hostOf(srv.URL) + "/32"}})
	// Request by hostname "localhost" (resolves to 127.0.0.1, unauthorized).
	ctx := WithRunInfo(context.Background(), RunInfo{TaskID: 101})
	_, err := doHTTP(ctx, httpCall{Method: "GET", URL: "http://localhost:" + portOf(srv.URL) + "/", NoJar: true, Timeout: 3 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("expected denial of unauthorized resolved address, got %v", err)
	}
}

func portOf(url string) string {
	_, p, _ := net.SplitHostPort(hostOf(url))
	return p
}

var _ = http.StatusOK
var _ = time.Second
