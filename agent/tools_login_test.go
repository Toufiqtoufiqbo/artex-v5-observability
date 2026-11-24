package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLoginCookieJarFlow proves the Login tool does two things: reports an
// authenticated verdict from the response, and leaves the session cookie in
// the shared jar so subsequent HTTPRequest calls to the same host go out
// authenticated.
func TestLoginCookieJarFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/account" {
			if "sid=ok1" == (r.Header.Get("Cookie")) {
				fmt.Fprint(w, "authenticated admin")
				return
			}
			w.WriteHeader(401)
			fmt.Fprint(w, "login required")
			return
		}
		if r.URL.Path != "/rest/user/login" {
			w.WriteHeader(404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var b struct {
			Email, Password string
		}
		_ = json.Unmarshal(body, &b)
		if b.Email == "admin" && b.Password == "x" {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "ok1", Path: "/"})
			fmt.Fprint(w, "ok")
			return
		}
		w.WriteHeader(401)
		fmt.Fprint(w, "no")
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)

	raw, _ := json.Marshal(map[string]any{
		"url": srv.URL + "/rest/user/login", "username": "admin", "password": "x",
		"username_param": "email", "json": true,
	})
	res, err := newLoginTool().Call(context.Background(), raw, nil)
	if err != nil || res.IsError {
		t.Fatalf("login call: err=%v out=%s", err, res.Flatten())
	}
	out := res.Flatten()
	if !strings.Contains(out, "authenticated=true") {
		t.Fatalf("expected authenticated=true:\n%s", out)
	}

	raw2, _ := json.Marshal(map[string]any{"url": srv.URL + "/account"})
	res2, err := runHTTPRequest(context.Background(), raw2, nil)
	if err != nil || res2.IsError {
		t.Fatalf("account call: err=%v out=%s", err, res2.Flatten())
	}
	if !strings.Contains(res2.Flatten(), "authenticated admin") {
		t.Fatalf("expected cookie to be replayed:\n%s", res2.Flatten())
	}
}

func TestLoginRejectsMissingCreds(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"url": "http://127.0.0.1/x"})
	res, err := newLoginTool().Call(context.Background(), raw, nil)
	if err == nil && !res.IsError {
		t.Fatalf("expected error for missing creds, got: %s", res.Flatten())
	}
}

func TestStrategyToolList(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"action": "list"})
	res, err := newStrategyTool().Call(context.Background(), raw, nil)
	if err != nil || res.IsError {
		t.Fatalf("strategy call: err=%v out=%s", err, res.Flatten())
	}
	for _, want := range []string{"RECON", "AUTHENTICATION", "AUTHZ / IDOR", "Probe", "CHAINING", "RETEST"} {
		if !strings.Contains(res.Flatten(), want) {
			t.Errorf("strategy output missing %q", want)
		}
	}
}
