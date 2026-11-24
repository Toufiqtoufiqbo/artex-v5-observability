package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIdorDiff: two distinct objects return distinct bodies — the tool should
// report 2 distinct responses with the ids that return each.
func TestIdorDiff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/users/") {
			id := strings.TrimPrefix(r.URL.Path, "/api/users/")
			switch id {
			case "10":
				fmt.Fprint(w, `{"user":10,"secret":"alice"}`)
			case "11":
				fmt.Fprint(w, `{"user":11,"secret":"bob"}`)
			default:
				fmt.Fprint(w, `{"error":"no such user"}`)
			}
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)

	raw, _ := json.Marshal(map[string]any{"url": srv.URL + "/api/users/{ID}", "id_range": "10-12"})
	res, err := newIdorDiffTool().Call(context.Background(), raw, nil)
	if err != nil || res.IsError {
		t.Fatalf("call: err=%v out=%s", err, res.Flatten())
	}
	out := res.Flatten()
	// 3 distinct objects: alice, bob, and the "no such user" default.
	if !strings.Contains(out, "distinct_responses=3") {
		t.Fatalf("expected 3 distinct responses:\n%s", out)
	}
}

// TestIdorDiffSame: when the endpoint returns the same body for every id,
// distinct_responses=1 and verdict is "no difference".
func TestIdorDiffSame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)
	raw, _ := json.Marshal(map[string]any{"url": srv.URL + "/same", "id_range": "1-3"})
	res, err := newIdorDiffTool().Call(context.Background(), raw, nil)
	if err != nil || res.IsError {
		t.Fatalf("call: err=%v out=%s", err, res.Flatten())
	}
	if !strings.Contains(res.Flatten(), "distinct_responses=1") {
		t.Fatalf("expected distinct_responses=1:\n%s", res.Flatten())
	}
}

// TestFuzz: standard SQLi payload set against a server with distinct responses
// per payload → tool should list distinct (status,len,fp) groups.
func TestFuzz(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		n := 1
		if strings.Contains(q, "1=1") {
			n = 2
		}
		if strings.Contains(q, "1=2") {
			n = 3
		}
		fmt.Fprint(w, strings.Repeat("z", 100*n))
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)
	raw, _ := json.Marshal(map[string]any{"url": srv.URL + "/p", "param": "q", "class": "sql_injection"})
	res, err := newFuzzTool().Call(context.Background(), raw, nil)
	if err != nil || res.IsError {
		t.Fatalf("call: err=%v out=%s", err, res.Flatten())
	}
	out := res.Flatten()
	if !strings.Contains(out, "distinct=") {
		t.Fatalf("expected distinct= output:\n%s", out)
	}
	t.Log(out)
}
