package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestExtractTokenMethodScope_v863 pins the N1/F-E2 hardening: ambient cookie
// and URL credentials are accepted only on safe methods (SSE is a GET), never on
// a state-changing request.
func TestExtractTokenMethodScope_v863(t *testing.T) {
	const tok = "synthetic.jwt.value"
	cases := []struct {
		name, method, header, cookie, query, want string
	}{
		{"post bearer", http.MethodPost, "Bearer " + tok, "", "", tok},
		{"post cookie", http.MethodPost, "", tok, "", ""},
		{"post query", http.MethodPost, "", "", tok, ""},
		{"patch cookie", http.MethodPatch, "", tok, "", ""},
		{"delete query", http.MethodDelete, "", "", tok, ""},
		{"put cookie", http.MethodPut, "", tok, "", ""},
		{"get cookie", http.MethodGet, "", tok, "", tok},
		{"get query", http.MethodGet, "", "", tok, tok},
		{"head query", http.MethodHead, "", "", tok, tok},
		{"get bearer", http.MethodGet, "Bearer " + tok, "", "", tok},
		{"get none", http.MethodGet, "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/api/exploration/findings/1", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			if tc.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "artex_token", Value: tc.cookie})
			}
			if tc.query != "" {
				q := r.URL.Query()
				q.Set("token", tc.query)
				r.URL.RawQuery = q.Encode()
			}
			if got := extractToken(r); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestRequireAuthRejectsCookieStateChange_v863 proves the middleware, not only
// extractToken, rejects a cookie/query-authenticated mutation before the handler
// runs, while still accepting Bearer mutations and cookie GETs.
func TestRequireAuthRejectsCookieStateChange_v863(t *testing.T) {
	key := []byte("synthetic-test-key-0123456789abcdef")
	s := &Server{jwtKey: key}
	token, err := signJWT(key)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	h := s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	// Cookie-authenticated POST is rejected before the handler runs.
	r := httptest.NewRequest(http.MethodPost, "/api/exploration/findings/1", nil)
	r.AddCookie(&http.Cookie{Name: "artex_token", Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || called {
		t.Fatalf("cookie POST code=%d called=%v", w.Code, called)
	}

	// Query-authenticated DELETE is likewise rejected.
	r = httptest.NewRequest(http.MethodDelete, "/api/exploration/findings/1?token="+token, nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || called {
		t.Fatalf("query DELETE code=%d called=%v", w.Code, called)
	}

	// Bearer POST works.
	r = httptest.NewRequest(http.MethodPost, "/api/exploration/findings/1", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || !called {
		t.Fatalf("bearer POST code=%d called=%v", w.Code, called)
	}

	// Cookie GET still works for SSE.
	called = false
	r = httptest.NewRequest(http.MethodGet, "/api/exploration/findings/1", nil)
	r.AddCookie(&http.Cookie{Name: "artex_token", Value: token})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || !called {
		t.Fatalf("cookie GET code=%d called=%v", w.Code, called)
	}
}
