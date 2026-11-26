package agent

import (
	"context"

	"github.com/Autumn-27/artex/db"
)

// EvidenceVerifier is injected by the host and checks a fact's claimed artifact
// against evidence the process actually recorded. Agents cannot call it
// directly; it is consulted by prove_goal before a fact may mark a goal met.
// A nil verifier keeps the shape-only check (unit tests, hosts without a
// recording backend); a wired verifier makes the check task-local and real.
type EvidenceVerifier interface {
	VerifyFact(ctx context.Context, taskID int64, node *db.Node) error
}

func (t *ToolSet) SetEvidenceVerifier(v EvidenceVerifier)   { t.evidenceVerifier = v }
func (w *Worker) SetEvidenceVerifier(v EvidenceVerifier)    { w.evidenceVerifier = v }
func (p *Planner) SetEvidenceVerifier(v EvidenceVerifier)   { p.evidenceVerifier = v }
func (m *MainAgent) SetEvidenceVerifier(v EvidenceVerifier) { m.evidenceVerifier = v }
