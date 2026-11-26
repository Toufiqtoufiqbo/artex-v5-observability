package server

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func TestMaskedMCPServerHidesEnvAndForcesOperatorInsecure(t *testing.T) {
	t.Setenv(envMCPInsecureHosts, "")
	m := &db.MCPServer{
		Name: "ScopeSentry", Transport: "http", URL: "https://mcp.example.com/mcp",
		Env: json.RawMessage(`{"X-API-Key":"SHOULD_NOT_BE_RETURNED"}`), Insecure: true,
	}
	view := maskedMCPServer(m)
	if bytes.Contains([]byte(view.Env), []byte("SHOULD_NOT_BE_RETURNED")) {
		t.Fatalf("env value leaked through masked view: %s", view.Env)
	}
	if view.Insecure {
		t.Fatal("a stored insecure=true must never become effective without operator opt-in")
	}
	var kv map[string]string
	if err := json.Unmarshal(view.Env, &kv); err != nil {
		t.Fatal(err)
	}
	if kv["X-API-Key"] != mcpEnvMask {
		t.Fatalf("masked env = %v", kv)
	}

	t.Setenv(envMCPInsecureHosts, "mcp.example.com")
	if !maskedMCPServer(m).Insecure {
		t.Fatal("operator-allowlisted host should be reported as insecure")
	}
}

func TestMergeMaskedMCPEnvKeepsStoredSecrets(t *testing.T) {
	stored := json.RawMessage(`{"Authorization":"STORED_VALUE","FOO":"bar"}`)
	incoming := json.RawMessage(`{"Authorization":"********","FOO":"baz","NEW":"********"}`)
	merged := mergeMaskedMCPEnv(stored, incoming)
	var kv map[string]string
	if err := json.Unmarshal(merged, &kv); err != nil {
		t.Fatal(err)
	}
	if kv["Authorization"] != "STORED_VALUE" || kv["FOO"] != "baz" {
		t.Fatalf("merged env = %v", kv)
	}
	if _, ok := kv["NEW"]; ok {
		t.Fatal("a new key sent only as the mask sentinel must be dropped")
	}
}

func TestCustomToolHTTPTestTargetPolicy(t *testing.T) {
	t.Setenv(envCustomToolHTTPTestHosts, "")
	if err := checkCustomToolHTTPTestTarget("https://api.example.com/hook"); err == nil {
		t.Fatal("HTTP dry runs must be denied when no allowlist is configured")
	}
	t.Setenv(envCustomToolHTTPTestHosts, "api.example.com")
	if err := checkCustomToolHTTPTestTarget("https://api.example.com/hook"); err != nil {
		t.Fatalf("allowlisted target rejected: %v", err)
	}
	for _, u := range []string{
		"https://evil.example/hook",
		"http://127.0.0.1:8080/admin",
		"http://169.254.169.254/latest/meta-data/",
		"https://user:placeholder@api.example.com/hook",
	} {
		if err := checkCustomToolHTTPTestTarget(u); err == nil {
			t.Errorf("target %q should be denied", u)
		}
	}
}

func TestSanitizeHTTPTestBody(t *testing.T) {
	got := sanitizeHTTPTestBody("ok\x00\x1b[31m\nnext", 8)
	if !bytes.Contains([]byte(got), []byte("…(已截断)")) {
		t.Fatalf("long body was not truncated: %q", got)
	}
	if bytes.ContainsAny([]byte(got), "\x00\x1b") {
		t.Fatalf("control characters survived sanitization: %q", got)
	}
}
