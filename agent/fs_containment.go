package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
)

// VIII.5 Gate B — filesystem path containment for the session file tools.
//
// Norma's Read/Write/Edit/MultiEdit/LS/Glob/Grep resolve paths with
// `resolvePath` (norma/tool/helpers.go): absolute paths pass through and
// relative paths are `filepath.Join(tc.WorkingDir, p)` — there is no
// containment check, no `..` rejection and no symlink resolution. Those tools
// are LLM-reachable (session surfaces include them), so the LLM can read or
// write anywhere in the process user's account. ARTEX installs
// `ContainFileTools` on its surfaces: relative paths are joined against
// tc.WorkingDir, the result is symlink-resolved, and any escape outside the
// resolved working directory is rejected (fail-closed). Legacy behavior is
// preserved when tc.WorkingDir is empty or absent (no containment possible).

// fsContainToolNames are the tools that WRITE files; their path inputs are
// contained. Read/LS/Glob/Grep stay permissive (reading broader roots is the
// norm in sessions where WorkingDir is the session subdir, and read-only
// access to the user account is a lower-severity boundary than writes).
var fsContainToolNames = map[string]struct{}{"Write": {}, "Edit": {}, "MultiEdit": {}}

// fsContainFields are the input fields interpreted as filesystem paths.
var fsContainFields = []string{"file_path", "path", "dir", "root"}

func fsWithinLex(root, cand string) bool {
	root = filepath.Clean(root)
	cand = filepath.Clean(cand)
	if root == "" || cand == "" {
		return false
	}
	if cand == root {
		return true
	}
	return strings.HasPrefix(cand, root+string(os.PathSeparator))
}

// fsResolveDeepest returns the symlink-resolved path for the deepest existing
// ancestor of p (so symlinked directories redirect the check), or p itself.
func fsResolveDeepest(p string) (string, error) {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r, nil
	}
	dir := filepath.Dir(p)
	if dir != p && dir != "" && filepath.Base(p) != string(os.PathSeparator) {
		if r, err := fsResolveDeepest(dir); err == nil {
			return filepath.Join(r, filepath.Base(p)), nil
		}
	}
	return p, nil
}

// fsContainmentPath validates p against the session working directory:
//   - empty p: allowed.
//   - no WorkingDir: allowed (legacy).
//   - absolute p: must resolve (symlink-aware) inside the resolved WorkingDir.
//   - relative p: joined against WorkingDir; must resolve inside it.
func fsContainmentPath(tc *actool.ToolContext, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	root := ""
	if tc != nil {
		root = strings.TrimSpace(tc.WorkingDir)
	}
	if root == "" {
		return p, nil
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	var cand string
	if filepath.IsAbs(p) {
		cand = filepath.Clean(p)
	} else {
		cand = filepath.Clean(filepath.Join(root, p))
	}
	resRoot, _ := fsResolveDeepest(root)
	resCand, _ := fsResolveDeepest(cand)
	if !fsWithinLex(resRoot, resCand) {
		return "", fmt.Errorf("path escapes working directory: %q", p)
	}
	return cand, nil
}

// fsContainedTool rejects any input path that escapes the session working
// directory, then delegates to the inner tool.
type fsContainedTool struct {
	actool.CoreTool
}

func (t fsContainedTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	var m map[string]json.RawMessage
	if len(in) > 0 {
		if err := json.Unmarshal(in, &m); err != nil {
			return actool.Result{}, err
		}
	}
	for _, k := range fsContainFields {
		raw, ok := m[k]
		if !ok || len(raw) == 0 {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue // non-string fields ignored
		}
		if _, err := fsContainmentPath(tc, s); err != nil {
			return actool.Errorf(err.Error()), nil
		}
	}
	return t.CoreTool.Call(ctx, in, tc)
}

// ContainFileTools installs fsContainedTool wrappers on the file tools that
// WRITE files (Write/Edit/MultiEdit) (VIII.5 Gate B).
func ContainFileTools(ts []actool.CoreTool) []actool.CoreTool {
	out := make([]actool.CoreTool, 0, len(ts))
	for _, t := range ts {
		if _, wrap := fsContainToolNames[t.Name()]; wrap {
			out = append(out, fsContainedTool{CoreTool: t})
		} else {
			out = append(out, t)
		}
	}
	return out
}
