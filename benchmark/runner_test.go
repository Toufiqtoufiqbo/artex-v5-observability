package benchmark

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
)

// runner_test.go — offline-mode integration through the REAL engine
// (Planner loop + Worker pool + report_finding). Skipped unless a benchmark
// DSN is configured (§16: disposable DB only).

func TestScriptedProviderEmitsScriptedTurns_v91(t *testing.T) {
	p := NewScriptedProvider([]ScriptResponse{
		{ToolUses: []ScriptToolUse{{Name: "report_finding", Input: []byte(`{"vulnclass":"SQLi"}`)}}},
		{Text: "已结束。"},
	})
	var events []llm.StreamEvent
	for i := 0; i < 2; i++ {
		for ev, err := range p.Stream(transcript.WithSessionID(context.Background(), "exp9-worker-i7"), llm.CompletionRequest{}) {
			if err != nil {
				t.Fatal(err)
			}
			events = append(events, ev)
		}
	}
	sawTool, sawText, sawDelta := 0, 0, false
	for _, ev := range events {
		switch ev.Type {
		case llm.SEToolUseStart:
			sawTool++
		case llm.SETextDelta:
			sawText++
		case llm.SEMessageDelta:
			sawDelta = true
		}
	}
	if sawTool != 1 || sawText != 1 || !sawDelta {
		t.Fatalf("scripted provider events: tool=%d text=%d delta=%v", sawTool, sawText, sawDelta)
	}
}

func TestOfflineRunnerFindingsIntegration_v91(t *testing.T) {
	if os.Getenv("ARTEX_PG_DSN") == "" {
		t.Skip("benchmark requires a disposable ARTEX_PG_DSN")
	}
	suite := SuiteByID("artex-v5-offline-baseline")
	if suite == nil {
		t.Fatal("default suite missing")
	}
	var c *Case
	for _, cc := range suite.Cases {
		if cc.ID == "sql_boolean_confirmed_local" {
			c = cc
		}
	}
	if c == nil {
		t.Fatal("case sql_boolean_confirmed_local not in suite")
	}
	runner, err := NewRunner(t.TempDir())
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer runner.Close()
	runner.SetProvider(NewScriptedProvider(c.Script))
	res, err := runner.RunCase(context.Background(), c)
	if err != nil {
		t.Fatalf("RunCase: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("status=%s errors=%v", res.Status, res.Errors)
	}
	if res.Metrics.TP != 1 {
		t.Fatalf("TP=%d want 1; actual=%v", res.Metrics.TP, res.ActualFindings)
	}
	if len(res.ActualFindings) != 1 || (res.ActualFindings[0].EvidenceState != "valid" && res.ActualFindings[0].EvidenceState != "none") {
		// "none" = no bound traffic in offline mode (no capture); "valid" = bound+verified.
		t.Fatalf("actual findings=%+v want 1 finding (valid or none evidence)", res.ActualFindings)
	}
	// Cleanup: remove the disposable task's findings + task (never other tasks).
	taskID, perr := strconv.ParseInt(res.TaskID, 10, 64)
	if perr == nil && taskID > 0 {
		_, _ = runner.pg.DeleteFindingsByTask(taskID)
		_ = runner.pg.DeleteTask(taskID)
	}
}
