package server

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/artex/db"
)

// VIII.5 Gate A — custom command-kind tools execute via actool.NewBash()
// (norma), which does NOT include ARTEX's bashGuardTool network-command guard.
// Repro: a custom tool with kind=command running a network-capable command is
// not blocked by the guard (defect). After the fix it is rejected.

func TestCustomToolCommandNetworkBlockedByGuard(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer m.Close()
	s := New(context.Background(), m, t.TempDir(), t.TempDir(), t.TempDir())

	exec := mustJSON5(t, map[string]any{
		"command": `Invoke-WebRequest -Uri http://127.0.0.1:1/ -UseBasicParsing -TimeoutSec 1; echo done`,
	})
	key := "gA-netcmd-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_ = m.pg.DeleteCustomTool(key) // clear any residue from previous runs
	if err := m.pg.CreateCustomTool(&db.Tool{Key: key, Kind: "command", Exec: exec, Agents: []string{"worker"}, Enabled: true}); err != nil {
		t.Fatalf("CreateCustomTool: %v", err)
	}
	t.Cleanup(func() { _ = m.pg.DeleteCustomTool(key) })

	tools, err := s.customTools()
	if err != nil {
		t.Fatalf("customTools: %v", err)
	}
	var tool actool.CoreTool
	for _, tl := range tools {
		if tl.Name() == key {
			tool = tl
			break
		}
	}
	if tool == nil {
		t.Fatalf("custom tool %q not in surface", key)
	}
	res, err := tool.Call(context.Background(), json.RawMessage(`{"args":""}`), nil)
	// VIII.5: the guard blocks with an error-result (actool.Errorf semantics:
	// IsError=true, nil Go error). Pre-fix, the tool ran the network command.
	if err == nil && (!res.IsError || !strings.Contains(res.Flatten(), "blocked")) {
		t.Fatalf("custom command tool ran network-capable command without guard (defect): res.IsError=%v err=%v", res.IsError, err)
	}
}

func mustJSON5(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
