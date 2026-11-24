package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGateFindingEvidence(t *testing.T) {
	cases := []struct {
		name      string
		evidence  string
		proof     string
		proofType string
		wantErr   bool
	}{
		{
			name:     "accepts concrete http exchange",
			evidence: "GET /rest/products/search?q=' OR '1'='1 -> 200 OK, 56 products returned (baseline 9)",
		},
		{
			name:     "accepts command output",
			evidence: "sqlmap -u 'http://127.0.0.1:3001/search?q=1' --batch -> parameter 'q' is vulnerable",
		},
		{
			name:     "accepts structured proof",
			evidence: "IDOR on /api/Users/25",
			proof:    "HTTP/1.1 200 OK\n{\"email\":\"passkey-user@juice-sh.op\"}",
			proofType: "response_body",
		},
		{
			name:     "accepts empty evidence (legacy contract)",
			evidence: "",
			wantErr:  false,
		},
		{
			name:     "rejects unconfirmed wording",
			evidence: "Prompt-injection decoys tested; no successful exploitation observed",
			wantErr:  true,
		},
		{
			name:     "rejects solution citation",
			evidence: "Payload: '+OR+1=1-- ; Lab solution confirms unreleased products displayed",
			wantErr:  true,
		},
		{
			name:      "rejects explicit none proof type (no artifact)",
			evidence:  "vulnerable parameter suspected",
			proofType: "none",
			wantErr:   true,
		},
		{
			name:     "rejects suspicion without artifact",
			evidence: "Parameter seems injectable, but could not trigger an exploit",
			wantErr:  true,
		},
		{
			name:     "accepts artifact without explicit proof_type",
			evidence: "GET /search?q=OR -> 200 with 56 items",
			wantErr:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gateFindingEvidence(tc.evidence, tc.proof, tc.proofType)
			if tc.wantErr && err == nil {
				t.Fatalf("expected rejection, got acceptance")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected acceptance, got: %v", err)
			}
		})
	}
}

// TestHTTPRequestTool proves the capability the workers were missing: a POST
// with a JSON body and a custom header, answered and reported intact. Before
// this tool existed the same probe went through Bash -> curl -> Invoke-WebRequest
// and failed on argument binding.
func TestHTTPRequestTool(t *testing.T) {
	var gotMethod, gotBody, gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("X-Echo", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	seedLoopbackScope(t, srv.URL)

	in, _ := json.Marshal(map[string]any{
		"method":  "POST",
		"url":     srv.URL + "/mcp",
		"headers": map[string]string{"Authorization": "Bearer test-token"},
		"body":    `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	})
	res, err := runHTTPRequest(context.Background(), in, nil)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %s", res.Flatten())
	}
	out := res.Flatten()

	if gotMethod != "POST" {
		t.Errorf("server saw method %q, want POST", gotMethod)
	}
	if !strings.Contains(gotBody, `"tools/list"`) {
		t.Errorf("server saw body %q", gotBody)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("server saw Authorization %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type auto-detect = %q, want application/json", gotCT)
	}
	for _, want := range []string{"201", "X-Echo: yes", `{"ok":true}`, "status=201"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestHTTPRequestToolRejectsBadURL keeps the tool from being used as a file or
// scheme escape hatch.
func TestHTTPRequestToolRejectsBadURL(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "gopher://127.0.0.1:6379/_INFO", "not-a-url"} {
		in, _ := json.Marshal(map[string]any{"url": u})
		res, err := runHTTPRequest(context.Background(), in, nil)
		if err != nil {
			t.Fatalf("%s: unexpected call error %v", u, err)
		}
		if !res.IsError {
			t.Errorf("%s: expected rejection, got success", u)
		}
	}
}
