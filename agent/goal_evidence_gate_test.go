package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func gateNode(kind, payload string) *db.Node {
	n := &db.Node{Kind: kind, ID: 7}
	if payload != "" {
		n.Payload = json.RawMessage(payload)
	}
	return n
}

func TestGateGoalEvidence(t *testing.T) {
	cases := []struct {
		name    string
		node    *db.Node
		wantErr string
	}{
		{
			name: "recorded finding always proves a goal",
			node: gateNode(db.KindFinding, `{"name":"SQL Injection in product search","severity":"high"}`),
		},
		{
			name: "fact with a request line and status",
			node: gateNode(db.KindFact, `{"summary":"login endpoint rejected the payload","detail":"POST /rest/user/login returned status=401"}`),
		},
		{
			name: "fact with a measured delta (negative result)",
			node: gateNode(db.KindFact, `{"summary":"no SQLi on product search","detail":"q=' AND '1'='2 returned 30 bytes while the baseline returned 26978 bytes"}`),
		},
		{
			name:    "bare assertion is not proof",
			node:    gateNode(db.KindFact, `{"summary":"the target looks vulnerable","detail":"probably injectable"}`),
			wantErr: "no reproducible artifact",
		},
		{
			name:    "published solution is not evidence",
			node:    gateNode(db.KindFact, `{"summary":"SQLi confirmed as shown in the solution","detail":"GET /x returned 200"}`),
			wantErr: "published solution",
		},
		{
			name:    "empty fact",
			node:    gateNode(db.KindFact, `{}`),
			wantErr: "empty",
		},
		{
			name:    "intent cannot prove a goal",
			node:    gateNode(db.KindIntent, `{"text":"assess the target"}`),
			wantErr: "must be a finding or a fact",
		},
		{
			name:    "nil evidence",
			node:    nil,
			wantErr: "missing",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := gateGoalEvidence(tc.node)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected evidence to pass, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestGoalMetAllowed(t *testing.T) {
	finding := &db.Node{Kind: db.KindFinding, ID: 1}
	metGoal := &db.Node{Kind: db.KindGoal, ID: 2, State: "met"}
	openGoal := &db.Node{Kind: db.KindGoal, ID: 3, State: "open"}

	if err := goalMetAllowed([]*db.Node{finding}, []*db.Node{openGoal}); err != nil {
		t.Fatalf("a recorded finding must allow goal_met: %v", err)
	}
	if err := goalMetAllowed(nil, []*db.Node{metGoal}); err != nil {
		t.Fatalf("all goals proven must allow goal_met: %v", err)
	}
	if err := goalMetAllowed(nil, []*db.Node{openGoal}); err == nil {
		t.Fatal("goal_met must be denied when no finding exists and a goal is unproven")
	}
	if err := goalMetAllowed(nil, nil); err == nil {
		t.Fatal("goal_met must be denied when nothing was recorded")
	}
	if err := goalMetAllowed(nil, []*db.Node{metGoal, nil}); err == nil {
		t.Fatal("goal_met must be denied on a nil goal node")
	}
}
