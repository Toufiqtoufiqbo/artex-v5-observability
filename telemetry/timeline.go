package telemetry

import (
	"fmt"
	"sort"
	"strings"
)

// Timeline builds a bounded, sorted execution timeline from the event log.
// Events are grouped by TaskID (or the global when TaskID == ""); each
// top-level section has start/completed timestamps and nested child events.
// Bounded: timelines for one task only; use ReadEvents + custom filters for
// full-graph views.
type TimelineNode struct {
	Event     *Event          `json:"event"`
	Children  []*TimelineNode `json:"children,omitempty"`
}

type Timeline struct {
	TaskID    string          `json:"task_id,omitempty"`
	Root      *TimelineNode   `json:"root,omitempty"`
	Flat      []*Event        `json:"flat,omitempty"`
}

// BuildTimeline reconstructs the execution order for one task (H2).
func BuildTimeline(evs []*Event, taskID string) Timeline {
	if taskID == "" {
		return Timeline{}
	}
	var flat []*Event
	root := &TimelineNode{}
	stack := []*TimelineNode{root}
	for _, ev := range evs {
		if ev.TaskID != taskID {
			continue
		}
		flat = append(flat, ev)
		node := &TimelineNode{Event: ev}
		stack[len(stack)-1].Children = append(stack[len(stack)-1].Children, node)
		switch ev.EventType {
		case WorkerExecutionStarted, PlannerPlanStarted, EngineTaskStarted:
			stack = append(stack, node)
		case WorkerExecutionCompleted, WorkerExecutionFailed,
			PlannerPlanCompleted, PlannerPlanFailed,
			EngineTaskCompleted, EngineTaskFailed, EngineTaskTimedOut, EngineTaskCancelled:
			// Pop back to the node this event closes (walk the stack).
			for i := len(stack) - 1; i > 0; i-- {
				if stack[i].Event != nil && stack[i].Event.EventID == ev.EventID {
					stack = stack[:i]
				}
			}
		}
	}
	return Timeline{TaskID: taskID, Root: root, Flat: flat}
}

// Durations returns total duration and counts per component/stage (H2 diagnostics).
func (t Timeline) Durations() (map[string]uint64, map[string]uint64) {
	totals := map[string]uint64{}
	counts := map[string]uint64{}
	for _, e := range t.Flat {
		key := e.Component
		if e.Stage != "" {
			key += ":" + e.Stage
		}
		if e.DurationMS != nil {
			totals[key] += uint64(*e.DurationMS)
			counts[key]++
		} else {
			counts[key+":0"]++
		}
	}
	return totals, counts
}

// String prints the timeline sorted by start time.
func (t Timeline) String() string {
	var b strings.Builder
	if t.TaskID != "" {
		fmt.Fprintf(&b, "task=%s events=%d\n", t.TaskID, len(t.Flat))
	}
	var walk func(n *TimelineNode, depth int)
	walk = func(n *TimelineNode, depth int) {
		for _, ch := range n.Children {
			ind := strings.Repeat("  ", depth)
			out := ""
			if ch.Event.DurationMS != nil {
				out = fmt.Sprintf(" (%dms)", *ch.Event.DurationMS)
			}
			fmt.Fprintf(&b, "%s%s %s%s\n", ind, ch.Event.StartedAt.Format("15:04:05"), ch.Event.EventType, out)
			walk(ch, depth+1)
		}
	}
	walk(t.Root, 0)
	return b.String()
}

// SortFlat sorts events by StartedAt ascending (B2 chronological order).
func (t Timeline) SortFlat() { sort.Slice(t.Flat, func(i, j int) bool { return t.Flat[i].StartedAt.Before(t.Flat[j].StartedAt) }) }
