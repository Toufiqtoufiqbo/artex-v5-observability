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

func probe(t *testing.T, in map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(in)
	res, err := runProbe(context.Background(), raw)
	if err != nil {
		t.Fatalf("runProbe: %v", err)
	}
	if res.IsError {
		t.Fatalf("probe error: %s", res.Flatten())
	}
	return res.Flatten()
}

func TestProbeSQLiBoolean(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		n := 9
		if strings.Contains(q, "OR") {
			n = 56
		}
		if strings.Contains(q, "AND") {
			n = 0
		}
		fmt.Fprint(w, strings.Repeat("x", 100*n))
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)

	out := probe(t, map[string]any{
		"kind": "sqli_boolean", "url": srv.URL + "/search", "param": "q",
	})
	if !strings.Contains(out, "VERDICT: CONFIRMED") {
		t.Fatalf("expected CONFIRMED:\n%s", out)
	}
	if !strings.Contains(out, "distinct_responses: baseline=1 true=1 false=1") {
		t.Fatalf("expected measurements in output:\n%s", out)
	}
}

func TestProbeSQLiBooleanNegative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "constant response, parameter ignored")
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)

	out := probe(t, map[string]any{
		"kind": "sqli_boolean", "url": srv.URL + "/search", "param": "q",
	})
	if !strings.Contains(out, "VERDICT: NOT CONFIRMED") {
		t.Fatalf("expected NOT CONFIRMED:\n%s", out)
	}
}

func TestProbeXSSReflect(t *testing.T) {
	raw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<html><body>%s</body></html>", r.URL.Query().Get("q"))
	}))
	defer raw.Close()
	seedLoopbackScope(t, raw.URL)

	out := probe(t, map[string]any{"kind": "xss_reflect", "url": raw.URL + "/", "param": "q"})
	if !strings.Contains(out, "VERDICT: CONFIRMED") {
		t.Fatalf("expected raw reflection to confirm:\n%s", out)
	}

	safe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<html><body>%s</body></html>", htmlEscape(r.URL.Query().Get("q")))
	}))
	defer safe.Close()
	seedLoopbackScope(t, safe.URL)

	out = probe(t, map[string]any{"kind": "xss_reflect", "url": safe.URL + "/", "param": "q"})
	if !strings.Contains(out, "VERDICT: NOT CONFIRMED") {
		t.Fatalf("expected encoded reflection to be rejected:\n%s", out)
	}
}

func TestProbeSSTI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("name")
		q = strings.ReplaceAll(q, "${7*7}", "49")
		q = strings.ReplaceAll(q, "{{7*7}}", "49")
		fmt.Fprint(w, "<html>"+q+"</html>")
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)

	out := probe(t, map[string]any{"kind": "ssti_eval", "url": srv.URL + "/greet", "param": "name"})
	if !strings.Contains(out, "VERDICT: CONFIRMED") {
		t.Fatalf("expected SSTI confirmed:\n%s", out)
	}
}

func TestProbeTraversal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("file"), "etc/passwd") {
			fmt.Fprint(w, "root:x:0:0:root:/root:/bin/bash")
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)

	out := probe(t, map[string]any{"kind": "path_traversal", "url": srv.URL + "/asset", "param": "file"})
	if !strings.Contains(out, "VERDICT: CONFIRMED") {
		t.Fatalf("expected traversal confirmed:\n%s", out)
	}
}

// TestSessionCookieJar proves the login-once behaviour: a cookie set by the
// first response is replayed automatically on the next call to the same host.
func TestSessionCookieJar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc123", Path: "/"})
			fmt.Fprint(w, "ok")
		case "/whoami":
			c, err := r.Cookie("sid")
			if err != nil || c.Value != "abc123" {
				w.WriteHeader(401)
				fmt.Fprint(w, "no session")
				return
			}
			fmt.Fprint(w, "authenticated")
		}
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)

	call := func(path string) string {
		raw, _ := json.Marshal(map[string]any{"url": srv.URL + path, "method": "POST"})
		if path == "/whoami" {
			raw, _ = json.Marshal(map[string]any{"url": srv.URL + path})
		}
		res, err := runHTTPRequest(context.Background(), raw, nil)
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		return res.Flatten()
	}

	call("/login")
	out := call("/whoami")
	if !strings.Contains(out, "authenticated") {
		t.Fatalf("cookie was not replayed:\n%s", out)
	}
}

func TestCrawl(t *testing.T) {
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			fmt.Fprint(w, "<html><title>Other</title><body>ok</body></html>")
			return
		}
		fmt.Fprintf(w, `<html><head><title>Home</title>
			<script>var api="/api/users"; var x='/rest/products';</script></head>
			<body><a href="/about">About</a>
			<form method="post" action="/login"><input name="email"><input name="password"></form>
			</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL
	seedLoopbackScope(t, srv.URL)

	raw, _ := json.Marshal(map[string]any{"url": base + "/", "max_pages": 5, "depth": 1})
	res, err := runCrawl(context.Background(), raw)
	if err != nil {
		t.Fatalf("runCrawl: %v", err)
	}
	if res.IsError {
		t.Fatalf("crawl error: %s", res.Flatten())
	}
	out := res.Flatten()
	for _, want := range []string{"/about", "/api/users", "/rest/products", "/login", "email", "Home", "classification", "params", "confidence"} {
		if !strings.Contains(out, want) {
			t.Errorf("crawl output missing %q:\n%s", want, out)
		}
	}
}

func TestClassifyPath(t *testing.T) {
	cases := map[string]string{
		"/rest/user/login": "auth",
		"/admin/dashboard": "admin",
		"/api/users/{ID}":  "api",
		"/app/main.js":     "static",
		"/search?q=x":      "search",
		"/":                "public",
		"/products/1":      "public",
	}
	for p, want := range cases {
		if got := classifyPath(p); got != want {
			t.Errorf("classifyPath(%q) = %q, want %q", p, got, want)
		}
	}
}
