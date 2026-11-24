package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
)

// Phase IX.2.2 — direct Planner / add_intent tests.
//
// These exercise the planner's own tool path (not just db.AddIntentPlanned):
// the add_intent tool handler, its batch contract, and one real Planner.Plan
// round driven by a scripted provider. Requires a disposable PostgreSQL DB.

func newPlannerIntentTools(t *testing.T, marker string) (*db.DB, *db.ExplorationStore, *ToolSet) {
	t.Helper()
	d := testDB(t)
	t.Cleanup(func() { _ = d.Close() })
	task, err := d.CreateTask("[ix222-agent] "+marker, "planner intent test goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(task.ID) })
	ts := d.Exploration(task.ExplorationID)
	tools := NewToolSet(ts, "planner")
	tools.SetTaskID(task.ID)
	return d, ts, tools
}

func callAddIntent(t *testing.T, tools *ToolSet, payload string) string {
	t.Helper()
	tool := tools.addIntent()
	res, err := tool.Call(context.Background(), json.RawMessage(payload), nil)
	if err != nil {
		t.Fatalf("add_intent call: %v", err)
	}
	return res.Flatten()
}

func countStoreIntents(t *testing.T, ts *db.ExplorationStore) int {
	t.Helper()
	nodes, err := ts.ListByKind(db.KindIntent, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return len(nodes)
}

// add_intent single form: the second identical submission reports reuse instead
// of silently creating a second row.
func TestAddIntentToolSingleDuplicate_v922(t *testing.T) {
	_, ts, tools := newPlannerIntentTools(t, "single-dup")

	first := callAddIntent(t, tools, `{"summary":"对 /login 做 SQL 注入测试"}`)
	if !strings.Contains(first, "intent created:") {
		t.Fatalf("first result = %q, want created", first)
	}
	second := callAddIntent(t, tools, `{"summary":"对 /login 做 SQL 注入测试"}`)
	if !strings.Contains(second, "outcome=duplicate") {
		t.Fatalf("second result = %q, want duplicate outcome", second)
	}
	if got := countStoreIntents(t, ts); got != 1 {
		t.Fatalf("intent rows = %d, want 1", got)
	}
}

// add_intent batch form: ids/outcomes stay index-aligned, and an in-batch
// duplicate reuses the earlier id.
func TestAddIntentToolBatchOutcomes_v922(t *testing.T) {
	_, ts, tools := newPlannerIntentTools(t, "batch")

	raw := callAddIntent(t, tools, `{"intents":[
		{"summary":"枚举 /api 端点"},
		{"summary":"枚举 /api 端点"},
		{"summary":"测试 /upload 文件上传绕过"}
	]}`)
	var out struct {
		IDs      []int64           `json:"ids"`
		Outcomes []string          `json:"outcomes"`
		Errors   map[string]string `json:"errors"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	if len(out.IDs) != 3 || len(out.Outcomes) != 3 {
		t.Fatalf("ids=%v outcomes=%v, want 3 aligned entries", out.IDs, out.Outcomes)
	}
	if out.Outcomes[0] != db.IntentOutcomeNew || out.Outcomes[1] != db.IntentOutcomeDuplicate || out.Outcomes[2] != db.IntentOutcomeNew {
		t.Fatalf("outcomes = %v, want new/duplicate/new", out.Outcomes)
	}
	if out.IDs[0] != out.IDs[1] {
		t.Fatalf("in-batch duplicate ids = %d/%d, want same", out.IDs[0], out.IDs[1])
	}
	if out.IDs[0] == out.IDs[2] {
		t.Fatalf("distinct work merged: %d", out.IDs[0])
	}
	if len(out.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", out.Errors)
	}
	if got := countStoreIntents(t, ts); got != 2 {
		t.Fatalf("intent rows = %d, want 2", got)
	}
}

// The per-call batch cap is administrative and model-unraisable.
func TestAddIntentToolBatchCap_v922(t *testing.T) {
	_, _, tools := newPlannerIntentTools(t, "batch-cap")

	items := make([]string, 0, db.MaxIntentBatch+1)
	for i := 0; i <= db.MaxIntentBatch; i++ {
		items = append(items, fmt.Sprintf(`{"summary":"方向 %d"}`, i))
	}
	raw := callAddIntent(t, tools, `{"intents":[`+strings.Join(items, ",")+`]}`)
	if !strings.Contains(raw, "一次最多提交") {
		t.Fatalf("oversized batch not rejected: %q", raw)
	}
}

// Structured operation/resource identity works through the tool, not just the DB.
func TestAddIntentToolStructuredDedup_v922(t *testing.T) {
	_, ts, tools := newPlannerIntentTools(t, "structured")

	callAddIntent(t, tools, `{"summary":"测试 /login 的布尔盲注","operation":"sqli-test","resource":"/login?id=1"}`)
	raw := callAddIntent(t, tools, `{"summary":"SQL injection on the login endpoint","operation":"sqli-test","resource":"/login?id=1"}`)
	if !strings.Contains(raw, "outcome=duplicate") {
		t.Fatalf("equivalent wording not merged: %q", raw)
	}
	if got := countStoreIntents(t, ts); got != 1 {
		t.Fatalf("intent rows = %d, want 1", got)
	}
}

// Dependencies are validated at the tool boundary with a readable error.
func TestAddIntentToolDependencyValidation_v922(t *testing.T) {
	_, _, tools := newPlannerIntentTools(t, "dep-invalid")

	raw := callAddIntent(t, tools, `{"summary":"依赖不存在的前置","depends_on":[999999]}`)
	if !strings.Contains(raw, "does not exist in this exploration") {
		t.Fatalf("missing prerequisite not reported: %q", raw)
	}
}

// A rejected proposal must not leave a partial intent row behind.
func TestAddIntentToolRejectLeavesNoRow_v922(t *testing.T) {
	_, ts, tools := newPlannerIntentTools(t, "no-partial")

	_ = callAddIntent(t, tools, `{"summary":"依赖不存在的前置","depends_on":[999999]}`)
	if got := countStoreIntents(t, ts); got != 0 {
		t.Fatalf("rejected proposal left %d row(s)", got)
	}
}

// Planner.Plan end-to-end: the model proposes the same intent twice in one
// session; the planner path must yield one intent row.
func TestPlannerPlanSuppressesDuplicateIntents_v922(t *testing.T) {
	d, ts, _ := newPlannerIntentTools(t, "plan-round")
	task, err := d.CreateTask("[ix222-agent] plan-round-owner", "planner round goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(task.ID) })
	store := d.Exploration(task.ExplorationID)

	turn := 0
	provider := captureUsageProvider{stream: func(_ context.Context, yield func(llm.StreamEvent, error) bool) {
		turn++
		var events []llm.StreamEvent
		if turn <= 2 {
			events = []llm.StreamEvent{
				{Type: llm.SEToolUseStart, ToolID: fmt.Sprintf("intent-%d", turn), ToolName: "add_intent"},
				{Type: llm.SEToolInputJSON, Text: `{"summary":"对 /login 做 SQL 注入测试","operation":"sqli-test","resource":"/login?id=1"}`},
				{Type: llm.SEMessageDelta, StopReason: "tool_use"},
			}
		} else {
			events = []llm.StreamEvent{
				{Type: llm.SETextDelta, Text: "规划完成"},
				{Type: llm.SEMessageDelta, StopReason: "end_turn"},
			}
		}
		for _, ev := range events {
			if !yield(ev, nil) {
				return
			}
		}
	}}

	pl := NewPlanner(provider, "test-model", t.TempDir(), nil, 0, 6)
	before := db.IntentPlanStats()
	if _, _, err := pl.Plan(context.Background(), task.ID, d.Assets(), store, "对目标做 SQL 注入测试", nil, nil); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	after := db.IntentPlanStats()

	if got := countStoreIntents(t, store); got != 1 {
		t.Fatalf("planner created %d intent rows, want 1", got)
	}
	if after.Duplicates <= before.Duplicates {
		t.Fatalf("duplicate counter did not advance: before=%d after=%d", before.Duplicates, after.Duplicates)
	}
	_ = ts
}

// Distinct intents proposed by the planner are preserved (no over-merge).
func TestPlannerPlanPreservesDistinctIntents_v922(t *testing.T) {
	d, _, _ := newPlannerIntentTools(t, "plan-distinct")
	task, err := d.CreateTask("[ix222-agent] plan-distinct-owner", "planner distinct goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(task.ID) })
	store := d.Exploration(task.ExplorationID)

	turn := 0
	provider := captureUsageProvider{stream: func(_ context.Context, yield func(llm.StreamEvent, error) bool) {
		turn++
		var events []llm.StreamEvent
		switch turn {
		case 1:
			events = []llm.StreamEvent{
				{Type: llm.SEToolUseStart, ToolID: "i1", ToolName: "add_intent"},
				{Type: llm.SEToolInputJSON, Text: `{"summary":"对 /login 做 SQL 注入测试","operation":"sqli-test","resource":"/login?id=1"}`},
				{Type: llm.SEMessageDelta, StopReason: "tool_use"},
			}
		case 2:
			events = []llm.StreamEvent{
				{Type: llm.SEToolUseStart, ToolID: "i2", ToolName: "add_intent"},
				{Type: llm.SEToolInputJSON, Text: `{"summary":"对 /upload 做文件上传绕过测试","operation":"upload-bypass","resource":"/upload"}`},
				{Type: llm.SEMessageDelta, StopReason: "tool_use"},
			}
		default:
			events = []llm.StreamEvent{
				{Type: llm.SETextDelta, Text: "规划完成"},
				{Type: llm.SEMessageDelta, StopReason: "end_turn"},
			}
		}
		for _, ev := range events {
			if !yield(ev, nil) {
				return
			}
		}
	}}

	pl := NewPlanner(provider, "test-model", t.TempDir(), nil, 0, 6)
	if _, _, err := pl.Plan(context.Background(), task.ID, d.Assets(), store, "测试登录与上传", nil, nil); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := countStoreIntents(t, store); got != 2 {
		t.Fatalf("distinct planner intents = %d, want 2", got)
	}
}
