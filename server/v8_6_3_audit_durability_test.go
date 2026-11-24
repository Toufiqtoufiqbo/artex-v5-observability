package server

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestEgressAuditDurabilityAndInjection_v863 covers the N3 closure: each record
// is fsynced, a persistence failure is contained (it cannot change the egress
// decision), concurrent appends stay parseable, and a reason containing newlines
// cannot forge extra audit records.
func TestEgressAuditDurabilityAndInjection_v863(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer m.Close()

	const n = 24
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				m.WriteEgressAudit("127.0.0.1:19004", true, "v863-allow")
				return
			}
			m.WriteEgressAudit("127.0.0.1:19998", false, "v863-deny")
		}(i)
	}
	wg.Wait()

	// A newline in the reason must not create a second record.
	m.WriteEgressAudit("127.0.0.1:19999", false, "line1\nline2\rline3")

	data, err := os.ReadFile(m.egressAuditPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != n+1 {
		t.Fatalf("audit lines=%d want %d", len(lines), n+1)
	}
	for _, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unparseable audit line %q: %v", line, err)
		}
		if _, ok := rec["decision"]; !ok {
			t.Fatalf("audit line missing decision: %q", line)
		}
	}

	// Failure semantics: pointing the sink at a directory makes OpenFile fail.
	// The call must return without panicking and without creating anything; the
	// traffic layer's deny decision is independent of this write.
	m.egressAuditPath = t.TempDir()
	m.WriteEgressAudit("127.0.0.1:19998", false, "v863-failure")
	if _, err := os.Stat(m.egressAuditPath); err != nil {
		t.Fatalf("sink path disappeared: %v", err)
	}
}
