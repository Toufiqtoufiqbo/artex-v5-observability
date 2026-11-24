package server

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
)

// VIII.6.1 Gate B — production egress audit sink persists decisions.
func TestEgressAuditSinkPersistsDecisions_v861(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer m.Close()
	m.WriteEgressAudit("127.0.0.1:19998", false, "v861-test-deny")
	m.WriteEgressAudit("127.0.0.1:19004", true, "v861-test-allow")
	data, err := os.ReadFile(m.egressAuditPath)
	if err != nil {
		t.Fatalf("audit file: %v", err)
	}
	text := string(data)
	for _, want := range []string{`"host":"127.0.0.1:19998"`, `"decision":"deny"`, `"host":"127.0.0.1:19004"`, `"decision":"allow"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("persisted audit missing %q: %s", want, text)
		}
	}
	st, err := os.Stat(m.egressAuditPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("audit file permissions too broad: %v", st.Mode().Perm())
	}
	_ = context.Background()
}
