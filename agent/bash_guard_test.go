package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

// VIII.4.2 — containment: network-capable Bash commands rejected; local
// commands delegate unchanged. Tests use the fake inner Bash tool, so no real
// process is spawned and the guard decision is fully deterministic.

func TestBashGuard_RejectsNetworkCommands(t *testing.T) {
	cmds := []string{
		`curl -s http://127.0.0.1:3004/ok`,
		`wget http://10.0.0.1/x -O -`,
		`ssh 192.168.1.10 echo hi`,
		`ping -c 2 10.0.0.5`,
		`nslookup example.com`,
		`dig @8.8.8.8 example.com`,
		`invoke-webrequest http://127.0.0.1/x`,
		`Invoke-RestMethod https://integrate.api.nvidia.com/v1/models`,
		`curl.exe -s http://example.com`,
		`scp file user@host:/tmp`,
		`/usr/bin/curl -v http://localhost/x`,
		`openssl s_client -connect example.com:443`,
	}
	for _, cmd := range cmds {
		if !bashNetworkCommand(cmd) {
			t.Errorf("bashNetworkCommand(%q) = false, want true", cmd)
		}
	}
}

func TestBashGuard_AllowsLocalCommands(t *testing.T) {
	cmds := []string{
		`echo ok`,
		`ls -la`,
		`grep -rn "ping" .`,
		`python -c "print(1+1)"`,
		`cat file.txt`,
		`git status`,
		`find . -name "*.go"`,
		`node -e "console.log('hi')"`,
		`curlgen.sh`,
		`shovel -v 1.2.3`,
	}
	for _, cmd := range cmds {
		if bashNetworkCommand(cmd) {
			t.Errorf("bashNetworkCommand(%q) = true, want false", cmd)
		}
	}
}

func TestBashGuardTool_GuardDecisions(t *testing.T) {
	fake := &fakeBashTool{}
	g := newBashGuardTool(fake)

	// network command → rejected, inner not called
	in, _ := json.Marshal(map[string]string{"command": "curl -s http://127.0.0.1:3004/ok"})
	res, err := g.Call(context.Background(), in, nil)
	if err != nil {
		t.Fatalf("unexpected call error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("network command must be rejected, got: %s", res.Flatten())
	}
	if !strings.Contains(res.Flatten(), "network-capable commands are blocked") {
		t.Errorf("error message: %q", res.Flatten())
	}
	if fake.called {
		t.Errorf("inner Bash must not be called for network commands")
	}

	// local command → delegates
	in, _ = json.Marshal(map[string]string{"command": "echo ok"})
	res, err = g.Call(context.Background(), in, nil)
	if err != nil || res.IsError {
		t.Fatalf("local command rejected: err=%v out=%s", err, res.Flatten())
	}
	if !fake.called || !strings.Contains(res.Flatten(), "echo ok") {
		t.Errorf("inner Bash not called or output wrong: called=%v out=%q", fake.called, res.Flatten())
	}
}

func TestWorkerDefaultTools_ContainsBash(t *testing.T) {
	ts := workerDefaultTools()
	found := false
	for _, tl := range ts {
		if tl.Name() == "Bash" {
			found = true
			if _, ok := tl.(*bashGuardTool); !ok {
				t.Fatalf("Bash in worker toolset is not the guard (got %T)", tl)
			}
		}
	}
	if !found {
		t.Fatalf("worker toolset must contain Bash (guard)")
	}
}

// VIII.4.6 Gate E3 — default surface has no local shell; opt-in keeps the guard.
func TestContainLocalShell_DefaultRemovesBashAndMCP(t *testing.T) {
	t.Setenv("ARTEX_ALLOW_LOCAL_SHELL", "")
	bash := &fakeBashTool{name: "Bash"}
	mcp := &fakeBashTool{name: "mcp__browser__navigate"}
	read := &fakeBashTool{name: "Read"}
	out := ContainLocalShell([]actool.CoreTool{bash, mcp, read})
	if len(out) != 1 || out[0].Name() != "Read" {
		names := make([]string, 0, len(out))
		for _, tl := range out {
			names = append(names, tl.Name())
		}
		t.Fatalf("default surface = %v, want only Read", names)
	}
}

func TestContainLocalShell_OptInWrapsBash(t *testing.T) {
	t.Setenv("ARTEX_ALLOW_LOCAL_SHELL", "1")
	bash := &fakeBashTool{name: "Bash"}
	out := ContainLocalShell([]actool.CoreTool{bash})
	if len(out) != 1 {
		t.Fatalf("opt-in surface len = %d, want 1", len(out))
	}
	if _, ok := out[0].(*bashGuardTool); !ok {
		t.Fatalf("opt-in Bash must be the network-command guard, got %T", out[0])
	}
}

func TestDropMCPTools(t *testing.T) {
	fakeMCP := &fakeBashTool{}
	fakeMCP.name = "mcp__browser__navigate"
	fakeWebFetch := &fakeBashTool{}
	fakeWebFetch.name = "WebFetch"
	fakeBash := &fakeBashTool{}
	fakeBash.name = "Bash"
	fakeRead := &fakeBashTool{}
	fakeRead.name = "Read"

	in := []actool.CoreTool{fakeMCP, fakeWebFetch, fakeBash, fakeRead}
	out := dropMCPTools(in)
	if len(out) != 3 {
		t.Fatalf("dropMCPTools len = %d, want 3", len(out))
	}
	for _, tl := range out {
		if tl.Name() == "mcp__browser__navigate" {
			t.Fatalf("mcp__ tool was not dropped")
		}
	}
}
