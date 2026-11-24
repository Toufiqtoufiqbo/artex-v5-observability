package agent

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/permission"
)

// ---------------------------------------------------------------------------
// VIII.4.2 — network-capable Bash containment
//
// The worker tool surface includes the SDK's Bash tool, whose commands can
// reach the network through shells (curl, wget, ping, ssh, ...) that this
// package does not see. The guard wraps Bash: commands that carry network
// egress intent are rejected with an explicit error; everything else delegates
// to the real Bash tool. The token pattern is a documented heuristic, tested
// below; it is a containment floor, not a policy engine.
// ---------------------------------------------------------------------------

var bashNetworkPattern = regexp.MustCompile(`(?i)(?:^|[\s/])(?:curl|wget|netcat|ncat|telnet|ping|ping6|nslookup|dig|whois|ssh|scp|sftp|rsync|nmap|masscan|bscan|iwr|invoke-webrequest|invoke-restmethod|invoke-webservice|openssl)(?:\.exe)?\b|/dev/tcp/`)

// bashNetworkCommand reports whether a shell command carries network egress
// intent. Token matching is word-bounded so ordinary file operations that
// merely mention a name are not blocked.
func bashNetworkCommand(command string) bool {
	return bashNetworkPattern.MatchString(command)
}

// LocalShellAllowed reports whether the operator explicitly opted in to local
// shell execution (VIII.4.6 Gate E3). Default false: Bash and the interactive
// shell family are removed from every agent surface, because a shell can spawn
// arbitrary interpreters/executables that this package cannot mediate.
func LocalShellAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ARTEX_ALLOW_LOCAL_SHELL"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// ContainLocalShell filters a final tool surface (VIII.4.6 Gate E3):
//   - Bash is removed unless LocalShellAllowed(); when allowed it is wrapped
//     by the network-command guard.
//   - mcp__* tools are removed (their destinations are not task-scope checked).
//
// The filter runs on the assembled surface, after skills/tools-table
// augmentation, so an alternate registration path cannot restore them.
func ContainLocalShell(ts []actool.CoreTool) []actool.CoreTool {
	out := make([]actool.CoreTool, 0, len(ts))
	for _, t := range ts {
		switch {
		case t.Name() == "Bash":
			if !LocalShellAllowed() {
				continue
			}
			out = append(out, newBashGuardTool(t))
		case strings.HasPrefix(t.Name(), "mcp__"):
			continue
		default:
			out = append(out, t)
		}
	}
	return out
}

// bashGuardTool wraps an inner CoreTool named "Bash" and rejects
// network-capable commands. Non-network commands delegate unchanged.
type bashGuardTool struct{ actool.CoreTool }

// NewBashGuardTool wraps a CoreTool named "Bash" (or any shell-like tool) so
// commands carrying network-egress intent are rejected (VIII.5 Gate A —
// custom command tools bypass surface containment, so they get the guard
// explicitly). Non-network commands delegate unchanged.
func NewBashGuardTool(inner actool.CoreTool) actool.CoreTool { return newBashGuardTool(inner) }

func newBashGuardTool(inner actool.CoreTool) actool.CoreTool {
	return &bashGuardTool{CoreTool: inner}
}

func (g *bashGuardTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	var a struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(in, &a)
	if a.Command != "" && bashNetworkCommand(a.Command) {
		return actool.Errorf("Bash: network-capable commands are blocked during autonomous tasks (VIII.4.2/VIII.4.4 containment)"), nil
	}
	return g.CoreTool.Call(ctx, in, tc)
}

// workerDefaultTools is the SDK default tool set for a Worker, trimmed as
// before, with Bash replaced by the VIII.4.2 network-command guard.
func workerDefaultTools() []actool.CoreTool {
	out := defaultToolsExcept("MultiEdit", "Glob", "Grep")
	for i, t := range out {
		if t.Name() == "Bash" {
			out[i] = newBashGuardTool(t)
		}
	}
	return out
}

// dropMCPTools removes MCP-discovered tools ("mcp__*") from the worker surface
// (VIII.4.2 containment: MCP destinations are not validated against the
// executing task's scope at this layer).
func dropMCPTools(ts []actool.CoreTool) []actool.CoreTool {
	out := make([]actool.CoreTool, 0, len(ts))
	for _, t := range ts {
		if strings.HasPrefix(t.Name(), "mcp__") {
			continue
		}
		out = append(out, t)
	}
	return out
}

// fakeBashTool records invocation; used by tests.
type fakeBashTool struct {
	name   string // defaults to "Bash"
	called bool
	last   string
}

func (f *fakeBashTool) Name() string {
	if f.name != "" {
		return f.name
	}
	return "Bash"
}
func (f *fakeBashTool) Description() string { return "fake bash (test)" }
func (f *fakeBashTool) Prompt() string      { return "" }
func (f *fakeBashTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (f *fakeBashTool) IsReadOnly(json.RawMessage) bool       { return false }
func (f *fakeBashTool) IsConcurrencySafe(json.RawMessage) bool { return true }
func (f *fakeBashTool) CheckPermissions(context.Context, json.RawMessage, permission.Context) permission.Decision {
	return permission.Allowed()
}
func (f *fakeBashTool) Call(_ context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
	f.called = true
	var a struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(in, &a)
	f.last = a.Command
	return actool.Text("ran: " + a.Command), nil
}
