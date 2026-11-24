package agent

import (
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

// VIII.5 Gate A — execution boundary tests (agent package, local-only).

func TestDefaultSurfacesHaveNoBash(t *testing.T) {
	t.Setenv("ARTEX_ALLOW_LOCAL_SHELL", "")
	surfaces := map[string][]actool.CoreTool{
		"default":           ContainLocalShell(actool.DefaultTools()),
		"worker":            ContainLocalShell(workerDefaultTools()),
		"shellSession":      ContainLocalShell(actool.ShellSessionTools()),
		"surfaceNoShell":    ContainLocalShell(defaultToolsExcept("MultiEdit", "Glob", "Grep")),
	}
	for name, ts := range surfaces {
		for _, tl := range ts {
			if tl.Name() == "Bash" || tl.Name() == "mcp__browser__navigate" {
				t.Errorf("surface %s must not contain %q by default", name, tl.Name())
			}
		}
	}
}

func TestBashGuardBlocksNativePowerShellNetCommands(t *testing.T) {
	t.Setenv("ARTEX_ALLOW_LOCAL_SHELL", "1")
	inner := &fakeBashTool{name: "Bash"}
	g := NewBashGuardTool(inner)
	netCmds := []string{
		"Invoke-WebRequest -Uri http://127.0.0.1:1/ -UseBasicParsing -TimeoutSec 1",
		"Invoke-RestMethod -Uri http://127.0.0.1:1/ -Method Get",
		"Invoke-WebService http://127.0.0.1:1/",
		"curl.exe -s http://127.0.0.1:1/",
		"powershell -Command Invoke-WebRequest http://127.0.0.1:1/ -UseBasicParsing",
		"powershell -NoProfile -NonInteractive -Command Invoke-RestMethod http://127.0.0.1:1/",
	}
	for _, cmd := range netCmds {
		in := []byte(`{"command":"` + cmd + `"}`)
		res, err := g.Call(t.Context(), in, nil)
		if err == nil && !res.IsError {
			t.Errorf("guard must block network command: %q", cmd)
		}
	}
	// Local commands delegate unchanged (error-result semantics: IsError=false).
	res, err := g.Call(t.Context(), []byte(`{"command":"echo ok"}`), nil)
	if err != nil || res.IsError {
		t.Errorf("guard must delegate local commands without blocking: err=%v res.IsError=%v", err, res.IsError)
	}
	if !inner.called {
		t.Fatal("guard must delegate local commands to the inner Bash tool")
	}
}
