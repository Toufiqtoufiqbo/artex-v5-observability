package agent

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// VIII.4.5.4 — connection reuse must not bypass destination authorization.

func TestPin_DialOnEveryRequest(t *testing.T) {
	srv := newLoopbackServerOn(t, "127.0.0.2", "ok")
	var dials int
	prev := dialFn
	dialFn = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials++
		return prev(ctx, network, addr)
	}
	t.Cleanup(func() { dialFn = prev })

	ClearTaskScope(); defer ClearTaskScope()
	SetTaskScopeForTask(101, []db.TaskScope{{Kind: "ip", Net: hostOf(srv.URL) + "/32"}})
	ctx := WithRunInfo(context.Background(), RunInfo{TaskID: 101})
	for i := 0; i < 2; i++ {
		res, err := doHTTP(ctx, httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second})
		if err != nil || res.Status != http.StatusOK {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if dials != 2 { // one dial per request; no reuse (DisableKeepAlives)
		t.Fatalf("expected a dial per request (no reuse), got %d", dials)
	}
}
