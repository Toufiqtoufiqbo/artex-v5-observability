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

func TestAttackPath(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"findings": []map[string]any{
			{"id": 1, "name": "SQLi on /a", "class": "sqli", "url": "http://h/a?x=1", "host": "h"},
			{"id": 2, "name": "IDOR on /b", "class": "idor", "url": "http://h/b/{ID}", "host": "h"},
			{"id": 3, "name": "SQLi on http://other/x", "class": "sqli", "url": "http://other/x"},
		},
	})
	res, err := newAttackPathTool().Call(context.Background(), raw, nil)
	if err != nil || res.IsError {
		t.Fatalf("call: err=%v out=%s", err, res.Flatten())
	}
	out := res.Flatten()
	if !strings.Contains(out, "SQLi on /a") || !strings.Contains(out, "IDOR on /b") {
		t.Fatalf("expected same-host chain:\n%s", out)
	}
	if strings.Contains(out, "http://other/x") {
		t.Logf("note: isolated host appears in output (no chain) — acceptable")
	}
	t.Log(out)
}

func TestRetest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		n := 1
		if strings.Contains(q, "OR") {
			n = 56
		}
		if strings.Contains(q, "AND") {
			n = 0
		}
		fmt.Fprint(w, strings.Repeat("x", n))
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)
	raw, _ := json.Marshal(map[string]any{
		"findings": []map[string]any{
			{"id": 7, "name": "SQLi", "class": "sqli_boolean", "url": srv.URL + "/p", "param": "q"},
		},
	})
	res, err := newRetestTool().Call(context.Background(), raw, nil)
	if err != nil || res.IsError {
		t.Fatalf("call: err=%v out=%s", err, res.Flatten())
	}
	if !strings.Contains(res.Flatten(), "CONFIRMED") {
		t.Fatalf("expected CONFIRMED:\n%s", res.Flatten())
	}
}
