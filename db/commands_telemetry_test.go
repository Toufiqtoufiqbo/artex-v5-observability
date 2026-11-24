package db

import "testing"

func TestToolCategoryForTelemetryBounded(t *testing.T) {
	cases := map[string]string{
		"":            "other",
		"Bash":        "shell",
		"mcp_shell":   "shell",
		"HTTPRequest": "web",
		"Read":        "read",
		"Write":       "write",
		"Edit":        "write",
		"unknown_tool": "other",
	}
	for in, want := range cases {
		if got := toolCategoryForTelemetry(in); got != want {
			t.Errorf("toolCategoryForTelemetry(%q) = %q, want %q", in, got, want)
		}
	}
}
