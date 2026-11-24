package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

// VIII.5 Gate B — filesystem write-path containment tests (agent package,
// local temp dirs only).

func callContained(t *testing.T, tool actool.CoreTool, in map[string]any, tc *actool.ToolContext) actool.Result {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Call(context.Background(), b, tc)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	return res
}

func TestFsContainmentWriteBlocksRelativeEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	rel, err := filepath.Rel(root, filepath.Join(outside, "evil.txt"))
	if err != nil || filepath.IsAbs(rel) {
		t.Fatalf("rel=%v err=%v", rel, err)
	}
	res := callContained(t, fsContainedTool{actool.NewWrite()}, map[string]any{"file_path": rel, "content": "x"}, &actool.ToolContext{WorkingDir: root})
	if !res.IsError || !strings.Contains(res.Flatten(), "escapes working directory") {
		t.Fatalf("relative write escape must be rejected, res=%+v", res)
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("escaped file was written")
	}
}

func TestFsContainmentWriteBlocksAbsoluteEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "evil.txt")
	res := callContained(t, fsContainedTool{actool.NewWrite()}, map[string]any{"file_path": target, "content": "x"}, &actool.ToolContext{WorkingDir: root})
	if !res.IsError || !strings.Contains(res.Flatten(), "escapes working directory") {
		t.Fatalf("absolute write escape must be rejected, res=%+v", res)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("escaped file was written")
	}
}

func TestFsContainmentWriteAllowsInsidePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "sub", "ok.txt")
	tc := &actool.ToolContext{WorkingDir: root}
	for _, p := range []string{"new.txt", "sub/ok.txt", inside} {
		res := callContained(t, fsContainedTool{actool.NewWrite()}, map[string]any{"file_path": p, "content": "x"}, tc)
		if res.IsError {
			t.Fatalf("inside write %q must be allowed: %v", p, res.Flatten())
		}
	}
}

func TestFsContainmentAllowsEmptyWorkingDir(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "anywhere.txt")
	res := callContained(t, fsContainedTool{actool.NewWrite()}, map[string]any{"file_path": target, "content": "x"}, &actool.ToolContext{WorkingDir: ""})
	if res.IsError {
		t.Fatalf("empty WorkingDir must preserve legacy writes: %v", res.Flatten())
	}
}

func TestContainFileToolsWrapsWriteTools(t *testing.T) {
	surfaces := ContainFileTools(actool.DefaultTools())
	for _, tl := range surfaces {
		switch tl.Name() {
		case "Write", "Edit", "MultiEdit":
			if _, ok := tl.(fsContainedTool); !ok {
				t.Errorf("surface tool %q must be the containment wrapper", tl.Name())
			}
		case "Read":
			if _, ok := tl.(fsContainedTool); ok {
				t.Error("Read is read-only and must not be wrapped")
			}
		}
	}
}

func TestFsContainmentEditBlocksEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("TOP-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(rel) {
		t.Fatalf("rel=%v err=%v", rel, err)
	}
	res := callContained(t, fsContainedTool{actool.NewEdit()}, map[string]any{"file_path": rel, "old_string": "TOP-SECRET", "new_string": "X"}, &actool.ToolContext{WorkingDir: root})
	if !res.IsError || !strings.Contains(res.Flatten(), "escapes working directory") {
		t.Fatalf("edit escape must be rejected, res=%+v", res)
	}
}
