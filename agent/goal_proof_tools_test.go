package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

// A source-task node may be visible through read-only views, but it must never
// satisfy prove_goal: doing so would let one task's evidence close another.
func TestProveGoalUsesTaskLocalEvidenceOnly(t *testing.T) {
	d := testDB(t)
	defer d.Close()

	source, err := d.CreateTask("[proof] source", "source goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	current, err := d.CreateTaskWithOptions("[proof] current", "current goal", db.TaskCreateOptions{SourceTaskIDs: []int64{source.ID}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = d.DeleteTask(current.ID)
		_ = d.DeleteTask(source.ID)
	})

	sourceFactID, err := d.Exploration(source.ExplorationID).AddNode(db.KindFact,
		map[string]any{"summary": "source observation", "detail": "GET /source returned HTTP/1.1 200"},
		0, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	localStore := d.Exploration(current.ExplorationID)
	goalID, err := localStore.AddNode(db.KindGoal, map[string]any{"text": "local goal"}, 0, "open", "planner", nil)
	if err != nil {
		t.Fatal(err)
	}

	tools := NewToolSet(localStore, "worker")
	tools.SetTaskID(current.ID)
	tools.SetEvidenceVerifier(&fakeEvidenceVerifier{})
	input := json.RawMessage(fmt.Sprintf(`{"goal_id":%d,"evidence_id":%d,"reason":"borrowed"}`, goalID, sourceFactID))
	res, err := tools.proveGoal().Call(context.Background(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "本任务") {
		t.Fatalf("source-task fact was accepted as proof: isError=%v text=%s", res.IsError, res.Flatten())
	}
	goal, err := localStore.GetNode(goalID)
	if err != nil || goal == nil || goal.State != "open" {
		t.Fatalf("goal state changed after denied proof: goal=%+v err=%v", goal, err)
	}
}

func TestGoalToolsRejectMalformedJSON(t *testing.T) {
	bad := json.RawMessage(`{"goal_id":`)
	tools := NewToolSet(&db.ExplorationStore{}, "")
	for name, tool := range map[string]actool.CoreTool{
		"prove_goal":  tools.proveGoal(),
		"goal_met":    tools.goalMet(),
		"record_fact": tools.recordFact(),
	} {
		t.Run(name, func(t *testing.T) {
			res, err := tool.Call(context.Background(), bad, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !res.IsError || !strings.Contains(res.Flatten(), "参数无效") {
				t.Fatalf("%s accepted malformed JSON: isError=%v text=%s", name, res.IsError, res.Flatten())
			}
		})
	}
}

func TestRecordFactBatchItemSchemaDeclaresFields(t *testing.T) {
	schema := NewToolSet(nil, "").recordFact().InputSchema()
	facts, ok := schema["properties"].(map[string]any)["facts"].(map[string]any)
	if !ok {
		t.Fatalf("facts schema missing: %#v", schema["properties"])
	}
	items, ok := facts["items"].(map[string]any)
	if !ok {
		t.Fatalf("facts.items schema missing: %#v", facts)
	}
	props, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatalf("facts.items.properties schema missing: %#v", items)
	}
	for _, field := range []string{"summary", "detail", "evidence", "confidence", "intent_id", "asset_ids", "traffic_refs"} {
		if _, ok := props[field]; !ok {
			t.Errorf("facts.items schema omits %q: %#v", field, props)
		}
	}
}
