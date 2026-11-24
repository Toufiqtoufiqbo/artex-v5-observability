package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// VIII.2.1 redirect boundary: every hop must be revalidated against scope.
func TestRedirectScopeRevalidated(t *testing.T) {
	ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Value: "127.0.0.1"}})
	defer ClearTaskScope()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.Header().Set("Location", "http://localhost:1/out") // different host -> out of scope
			w.WriteHeader(302)
			return
		}
		if r.URL.Path == "/good" {
			w.Header().Set("Location", "/out") // same host -> in scope
			w.WriteHeader(302)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	call := httpCall{Method: "GET", URL: srv.URL + "/bad", FollowRedirects: true, NoJar: true, Timeout: 5 * time.Second}
	if _, err := doHTTP(context.Background(), call); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("cross-origin redirect must be rejected by scope, got %v", err)
	}

	call.URL = srv.URL + "/good"
	res, err := doHTTP(context.Background(), call)
	if err != nil || res.Status != 200 {
		t.Fatalf("same-origin redirect should succeed, got err=%v res=%+v", err, res)
	}
}
