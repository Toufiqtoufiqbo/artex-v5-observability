package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// VIII.5 Gate C — synthetic-secret leak tests (all fixtures are synthetic;
// no real credentials are used).

const testSyntheticKey = "ARTEX_TEST_SECRET_7F2A"

func v5DoAPI(t *testing.T, h http.Handler, tok string, method, path string, body any) (int, string) {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

func TestSyntheticSecretsNotExposedViaProfilesAPI(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	root := t.TempDir()
	s := New(context.Background(), m, root, root, root)
	defer m.Close() // LIFO: close the server (and its goroutines) before root cleanup.
	tok, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()

	// Create a profile carrying a synthetic key (POST /api/llm/profiles).
	name := "gC-leak-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	code, body := v5DoAPI(t, h, tok, "POST", "/api/llm/profiles", map[string]any{
		"name": name, "format": "openai", "model": "m", "api_key": testSyntheticKey,
	})
	if code != 200 {
		t.Fatalf("create profile: code=%d body=%s", code, body)
	}

	// Read-back via the profiles API must not contain the synthetic key.
	code, body = v5DoAPI(t, h, tok, "GET", "/api/llm/profiles", nil)
	if code != 200 {
		t.Fatalf("list profiles: code=%d body=%s", code, body)
	}
	if strings.Contains(body, testSyntheticKey) {
		t.Fatalf("synthetic key leaked via GET /api/llm/profiles: %s", body)
	}
	var payload struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode profiles: %v (body=%s)", err, body)
	}
	for _, row := range payload.Profiles {
		if hint, _ := row["api_key_hint"].(string); strings.Contains(hint, testSyntheticKey) {
			t.Fatalf("synthetic key leaked via api_key_hint: %s", hint)
		}
	}
}

func TestAuthStatusDoesNotExposeSecrets(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	root := t.TempDir()
	s := New(context.Background(), m, root, root, root)
	defer m.Close()
	_, body := v5DoAPI(t, s.Handler(), "", "GET", "/api/auth/status", nil)
	if strings.Contains(body, testSyntheticKey) || strings.Contains(body, "password_hash") {
		t.Fatalf("auth status leaked secrets: %s", body)
	}
}
