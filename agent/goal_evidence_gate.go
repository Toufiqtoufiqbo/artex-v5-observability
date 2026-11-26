package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// The goal gate exists because prove_goal and goal_met were the last unguarded
// path to "done". The evidence gate on report_finding only covered findings, so
// a task could still end with zero findings by proving a goal with a bare fact
// ("the target looks vulnerable") or by calling goal_met with a free-text
// reason. Both are advisory-free escapes: no traffic, no measurement, no
// reproducible artifact.
//
// The rule here keeps legitimate negative results working. A fact that cites a
// measured observation (request line, status code, byte/record delta, tool
// output) is accepted, so "no SQLi on /rest/products/search: 200 with 30 bytes
// and the payload was rejected" still proves a goal. An unbacked assertion is
// refused. A recorded finding is always accepted.

// goalEvidenceText flattens the human-readable fields a node carries so the
// same artifact/solution checks used for findings can be applied to facts.
func goalEvidenceText(n *db.Node) string {
	if n == nil || len(n.Payload) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return string(n.Payload)
	}
	var b strings.Builder
	for _, key := range []string{"summary", "detail", "evidence", "name", "description", "proof", "reason"} {
		if s, ok := payload[key].(string); ok && strings.TrimSpace(s) != "" {
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String()
}

// gateGoalEvidence validates the evidence node attached to a goal.
func gateGoalEvidence(n *db.Node) error {
	if n == nil {
		return fmt.Errorf("evidence node is missing")
	}
	switch n.Kind {
	case db.KindFinding:
		return nil
	case db.KindFact:
		text := goalEvidenceText(n)
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("evidence fact is empty")
		}
		if solutionLeakRe.MatchString(text) {
			return fmt.Errorf("evidence cites a published solution/walkthrough instead of the tester's own traffic")
		}
		if !proofArtifactRe.MatchString(text) {
			return fmt.Errorf("evidence fact has no reproducible artifact: cite the request/response, status code, measured byte/record delta or tool output in detail/evidence, or record a finding")
		}
		return nil
	default:
		return fmt.Errorf("evidence must be a finding or a fact, got %q", n.Kind)
	}
}

// goalMetAllowed reports whether the unconditional goal_met shortcut may end
// the task. A finding is evidence by itself; with none recorded, every goal
// must already be proven through prove_goal (which requires an artifact).
func goalMetAllowed(findings, goals []*db.Node) error {
	if len(findings) > 0 {
		return nil
	}
	if len(goals) == 0 {
		return fmt.Errorf("no goal and no finding recorded: nothing to conclude")
	}
	for _, g := range goals {
		if g == nil {
			return fmt.Errorf("goal list contains a nil node")
		}
		if g.State != "met" {
			return fmt.Errorf("no finding recorded and goal %d is not proven; prove each goal with prove_goal (artifact required) or record a finding first", g.ID)
		}
	}
	return nil
}
