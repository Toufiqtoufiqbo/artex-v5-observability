package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// Security hardening: an editor "test" request must never run an unsaved exec
// spec. A command payload that would create a marker file is ignored unless a
// matching saved custom tool is referenced by key.
func TestCustomToolTestRequiresSavedTool(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer m.Close()
	s := New(t.Context(), m, t.TempDir(), t.TempDir(), t.TempDir())

	marker := filepath.Join(t.TempDir(), "unsaved-exec-ran")
	body := map[string]any{
		"kind":   "command",
		"exec":   map[string]any{"command": "printf poc > " + marker},
		"params": map[string]any{},
	}
	b, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	s.pgTestCustomTool(rec, httptest.NewRequest("POST", "/api/tools/custom/test", bytes.NewReader(b)))
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400 (unsaved tool must be rejected): %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("unsaved exec was executed (marker exists, stat err=%v)", err)
	}

	// A saved tool remains testable by key; its stored exec (not the request's)
	// is what runs, and a mismatched kind is refused.
	key := "saved_test_tool_" + strings.ToLower(t.Name())
	_ = m.pg.DeleteCustomTool(key)
	if err := m.pg.CreateCustomTool(&db.Tool{
		Key: key, Kind: "command", Exec: mustJSON5(t, map[string]any{"command": "echo saved-ok"}),
		Agents: []string{"worker"}, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateCustomTool: %v", err)
	}
	t.Cleanup(func() { _ = m.pg.DeleteCustomTool(key) })

	body["key"] = key
	body["kind"] = "script" // mismatch with the saved command tool
	b, _ = json.Marshal(body)
	rec = httptest.NewRecorder()
	s.pgTestCustomTool(rec, httptest.NewRequest("POST", "/api/tools/custom/test", bytes.NewReader(b)))
	if rec.Code != 400 {
		t.Fatalf("mismatched kind status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	body["kind"] = "command"
	b, _ = json.Marshal(body)
	rec = httptest.NewRecorder()
	s.pgTestCustomTool(rec, httptest.NewRequest("POST", "/api/tools/custom/test", bytes.NewReader(b)))
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("saved-ok")) {
		t.Fatalf("saved tool test: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
