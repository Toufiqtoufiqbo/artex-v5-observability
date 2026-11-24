package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"sync"

	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
)

// mock_provider.go — ScriptedProvider implements llm.Provider for offline
// (deterministic) benchmark runs. It emits the benchmark-authored script as
// normalized stream events, mirroring the conventions the harness and llmrec
// both consume (llm/accumulate.go, llm/provider.go, harness/query.go).
//
// Each Stream call pops the next scripted entry for the caller's lane. Lanes
// are derived from the transcript session id ("exp{N}-planner",
// "exp{N}-worker-i{M}"). Unscripted lanes receive the worker script; when the
// script is exhausted the provider returns a text-only "end_turn" response so
// loops always terminate (a documented default, not a fabricated TP).
//
// It is NOT a performance oracle — offline mode measures the ENGINE's behavior
// against ground truth; the model itself is scripted (package doc §5).

type ScriptToolUse struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ScriptResponse is one model turn scripted for the provider.
type ScriptResponse struct {
	Text     string          `json:"text,omitempty"`
	ToolUses []ScriptToolUse `json:"tool_uses,omitempty"`
	// Lane routes the entry: "planner", "worker", ... Entries without a lane
	// default to the "worker" lane.
	Lane string `json:"lane,omitempty"`
	// Usage is the token accounting attributed to this turn (offline mode;
	// live mode reads real provider values).
	Usage llm.Usage `json:"usage,omitempty"`
}

type ScriptedProvider struct {
	mu     sync.Mutex
	byLane map[string][]ScriptResponse
	idx    map[string]int
	calls  int
	id     int
}

func NewScriptedProvider(script []ScriptResponse) *ScriptedProvider {
	p := &ScriptedProvider{byLane: map[string][]ScriptResponse{}, idx: map[string]int{}}
	for _, r := range script {
		lane := r.Lane
		if lane == "" {
			lane = "worker"
		}
		p.byLane[lane] = append(p.byLane[lane], r)
	}
	return p
}

// Calls returns how many times the provider has been called (observability).
func (p *ScriptedProvider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// laneFromSession derives the agent lane from the transcript session id.
func laneFromSession(sid string) string {
	rest := strings.TrimPrefix(sid, "exp")
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		rest = rest[i+1:]
	} else {
		return "other"
	}
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		rest = rest[:i]
	}
	if rest == "" {
		return "other"
	}
	return rest
}

func (p *ScriptedProvider) popLocked(lane string) (ScriptResponse, bool) {
	entries := p.byLane[lane]
	if len(entries) == 0 && lane != "worker" {
		entries = p.byLane["worker"] // unscripted lane → worker script fallback
	}
	idx := p.idx[lane]
	if idx < len(entries) {
		p.idx[lane] = idx + 1
		return entries[idx], true
	}
	return ScriptResponse{}, false
}

func (p *ScriptedProvider) next(ctx context.Context, yield func(llm.StreamEvent, error) bool) {
	lane := laneFromSession(transcript.SessionIDFrom(ctx))
	p.mu.Lock()
	r, ok := p.popLocked(lane)
	if ok {
		p.calls++
	}
	p.mu.Unlock()
	if !ok {
		// Default: text-only end_turn response so the loop always terminates.
		yield(llm.StreamEvent{Type: llm.SEMessageStart, Usage: llm.Usage{InputTokens: 1}}, nil)
		yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: "end_turn"}, nil)
		yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
		return
	}
	if len(r.ToolUses) > 0 {
		for _, tu := range r.ToolUses {
			id := fmt.Sprintf("scripted_%d", p.id)
			p.id++
			yield(llm.StreamEvent{Type: llm.SEToolUseStart, ToolID: id, ToolName: tu.Name}, nil)
			if len(tu.Input) > 0 {
				yield(llm.StreamEvent{Type: llm.SEToolInputJSON, Text: string(tu.Input)}, nil)
			}
		}
	}
	if r.Text != "" {
		yield(llm.StreamEvent{Type: llm.SETextDelta, Text: r.Text}, nil)
	}
	yield(llm.StreamEvent{Type: llm.SEMessageStart, Usage: llm.Usage{InputTokens: r.Usage.InputTokens}}, nil)
	yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: stopReasonFor(r), Usage: llm.Usage{OutputTokens: r.Usage.OutputTokens}}, nil)
	yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
}

func stopReasonFor(r ScriptResponse) string {
	if len(r.ToolUses) > 0 {
		return "tool_use"
	}
	return "end_turn"
}

func (p *ScriptedProvider) Stream(ctx context.Context, _ llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) { p.next(ctx, yield) }
}

func (p *ScriptedProvider) Complete(ctx context.Context, _ llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	// Mirror Stream for NonStreaming mode; offline runs don't use it, but keep
	// the interface honest.
	var msg llm.Message
	for ev, err := range p.Stream(ctx, llm.CompletionRequest{}) {
		if err != nil {
			return msg, "", llm.Usage{}, err
		}
		switch ev.Type {
		case llm.SETextDelta:
			if len(msg.Content) == 0 || msg.Content[len(msg.Content)-1].Type != llm.BlockText {
				msg.Content = append(msg.Content, llm.TextBlock(ev.Text))
			} else {
				msg.Content[len(msg.Content)-1].Text += ev.Text
			}
		case llm.SEToolUseStart:
			msg.Content = append(msg.Content, llm.ContentBlock{Type: llm.BlockToolUse, ID: ev.ToolID, Name: ev.ToolName})
		case llm.SEToolInputJSON:
			if len(msg.Content) > 0 {
				b := &msg.Content[len(msg.Content)-1]
				b.Input = append(b.Input, ev.Text...)
			}
		}
	}
	return msg, "", llm.Usage{}, nil
}
