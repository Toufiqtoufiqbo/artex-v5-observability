package telemetry

import (
	"fmt"
	"sort"
	"sync"
)

// MetricType classifies a metric (Gate G2).
type MetricType int

const (
	Counter MetricType = iota
	Gauge
	Histogram
)

// String returns the stable name of the type.
func (m MetricType) String() string {
	switch m {
	case Counter:
		return "counter"
	case Gauge:
		return "gauge"
	case Histogram:
		return "histogram"
	}
	return "unknown"
}

// Metric documents one metric (Gate G2.2: documented definition, unit, labels).
type Metric struct {
	Name   string
	Type   MetricType
	Unit   string
	Help   string // required documentation
	Labels []string // allowed, bounded label keys (Gate G3)
}

// MetricValue is a snapshot of one metric series.
type MetricValue struct {
	Name        string
	Type        string
	Unit        string
	Help        string
	Labels      map[string]string
	Value       uint64
	Percentiles map[string]uint64 `json:"percentiles,omitempty"` // p50/p90/p99 (histograms)
}

// Registry is the bounded, concurrency-safe metrics store (Gate G1/G2).
// It never blocks execution: telemetry updates are atomic increments.
type Registry struct {
	mu      sync.RWMutex
	metrics map[string]*series
}

type series struct {
	def     Metric
	labels  map[string]string
	value   uint64
	samples []uint64 // bounded histogram samples (Cap at SampleLimit)
}

const SampleLimit = 1000

// NewRegistry returns an empty registry with the documented metric table (Gate G2.1).
func NewRegistry() *Registry {
	r := &Registry{metrics: map[string]*series{}}
	r.defineAll()
	return r
}

func (r *Registry) defineAll() {
	type def struct{ m Metric }
	table := []def{
		{m: Metric{Name: "engine.tasks.completed", Type: Counter, Unit: "count", Help: "Engine tasks completed by outcome."}},
		{Metric{Name: "engine.tasks.failed", Type: Counter, Unit: "count", Help: "Engine tasks failed by error_class."}},
		{Metric{Name: "engine.tasks.cancelled", Type: Counter, Unit: "count", Help: "Engine tasks cancelled."}},
		{Metric{Name: "engine.tasks.timed_out", Type: Counter, Unit: "count", Help: "Engine tasks that ended by timeout."}},
		{Metric{Name: "engine.tasks.active", Type: Gauge, Unit: "count", Help: "Engine tasks currently running (set by engine ticks)."}},
		{Metric{Name: "engine.tasks.duration_ms", Type: Histogram, Unit: "ms", Help: "Engine task total duration."}},
		{Metric{Name: "planner.plans", Type: Counter, Unit: "count", Help: "Planner rounds executed by outcome."}},
		{Metric{Name: "planner.duration_ms", Type: Histogram, Unit: "ms", Help: "Planner.Plan duration."}},
		{Metric{Name: "planner.retries", Type: Counter, Unit: "count", Help: "Planner rounds retried."}},
		{Metric{Name: "planner.intents_generated", Type: Counter, Unit: "count", Help: "Intents the planner generated."}},
		{Metric{Name: "planner.intents_accepted", Type: Counter, Unit: "count", Help: "Intents the planner created."}},
		{Metric{Name: "planner.intents_duplicates", Type: Counter, Unit: "count", Help: "Duplicate intents suppressed by dedup."}},
		{Metric{Name: "planner.goal_decompose_ms", Type: Histogram, Unit: "ms", Help: "Goal decomposition duration (DecomposeGoalsWithProvider)."}},
		{Metric{Name: "worker.executions", Type: Counter, Unit: "count", Help: "Worker executions by outcome."}},
		{Metric{Name: "worker.duration_ms", Type: Histogram, Unit: "ms", Help: "Worker.Execute/ExecuteWithMessage duration."}},
		{Metric{Name: "worker.tool_calls", Type: Counter, Unit: "count", Help: "Tool invocations during worker execution."}},
		{Metric{Name: "worker.tool_failures", Type: Counter, Unit: "count", Help: "Tool invocations that failed (by error_class)."}},
		{Metric{Name: "tool.duration_ms", Type: Histogram, Unit: "ms", Help: "Per-invocation tool duration by tool category."}},
		{Metric{Name: "tool.duration_ms[read]", Type: Histogram, Unit: "ms", Help: "Per-invocation tool duration (category: read)."}},
		{Metric{Name: "tool.duration_ms[write]", Type: Histogram, Unit: "ms", Help: "Per-invocation tool duration (category: write)."}},
		{Metric{Name: "tool.duration_ms[shell]", Type: Histogram, Unit: "ms", Help: "Per-invocation tool duration (category: shell)."}},
		{Metric{Name: "tool.duration_ms[web]", Type: Histogram, Unit: "ms", Help: "Per-invocation tool duration (category: web)."}},
		{Metric{Name: "tool.duration_ms[other]", Type: Histogram, Unit: "ms", Help: "Per-invocation tool duration (category: other)."}},
		{Metric{Name: "tool.failures", Type: Counter, Unit: "count", Help: "Tool invocations that failed, by category."}},
		{Metric{Name: "tool.invocations", Type: Counter, Unit: "count", Help: "Tool invocations seen by the diagnostics surface."}},
		{Metric{Name: "provider.calls", Type: Counter, Unit: "count", Help: "LLM provider calls by status."}},
		{Metric{Name: "provider.input_tokens", Type: Counter, Unit: "tokens", Help: "Input tokens reported by provider (0 = none reported; see unknown_usage_count)."}},
		{Metric{Name: "provider.output_tokens", Type: Counter, Unit: "tokens", Help: "Output tokens reported by provider."}},
		{Metric{Name: "provider.unknown_usage_count", Type: Counter, Unit: "count", Help: "Provider calls where token usage was not reported (USAGE_UNKNOWN)."}},
		{Metric{Name: "provider.errors", Type: Counter, Unit: "count", Help: "Provider calls returning an error status."}},
		{Metric{Name: "provider.latency_ms", Type: Histogram, Unit: "ms", Help: "Provider call latency by status."}},
		{Metric{Name: "finding.submitted", Type: Counter, Unit: "count", Help: "Findings submitted by outcome."}},
		{Metric{Name: "finding.duplicates", Type: Counter, Unit: "count", Help: "Duplicate finding submissions suppressed."}},
		{Metric{Name: "finding.reused", Type: Counter, Unit: "count", Help: "Existing findings reused by dedup."}},
		{Metric{Name: "finding.persistence_failures", Type: Counter, Unit: "count", Help: "Findings that failed to persist."}},
		{Metric{Name: "evidence.validations", Type: Counter, Unit: "count", Help: "Evidence validations by state."}},
		{Metric{Name: "evidence.failures", Type: Counter, Unit: "count", Help: "Evidence validation failures by reason."}},
		{Metric{Name: "telemetry.write_failures", Type: Counter, Unit: "count", Help: "Telemetry store write failures (telemetry never fails execution)."}},
		{Metric{Name: "telemetry.overhead_ms", Type: Histogram, Unit: "ms", Help: "Approximate event-emission overhead per operation (instrumented call site)."}},
	}
	for _, d := range table {
		key := d.m.Name
		if _, ok := r.metrics[key]; !ok {
			r.metrics[key] = &series{def: d.m}
		}
	}
}

// Metric returns the documented definition of a metric by name.
func (r *Registry) Metric(name string) (Metric, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.metrics[name]
	if !ok {
		return Metric{}, false
	}
	return s.def, true
}

// AllMetricDefs returns all documented metric definitions.
func (r *Registry) AllMetricDefs() []Metric {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Metric, 0, len(r.metrics))
	for _, s := range r.metrics {
		out = append(out, s.def)
	}
	return out
}

func (r *Registry) seriesLocked(name string, labels map[string]string) *series {
	s := r.metrics[name]
	if s == nil {
		s = &series{def: Metric{Name: name}}
		r.metrics[name] = s
	}
	if labels != nil {
		s.labels = labels
	}
	return s
}

// Inc increments a counter series (safe on nil registry — no-op).
func (r *Registry) Inc(name string, labels map[string]string, n uint64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	s := r.seriesLocked(name, labels)
	s.value += n
	r.mu.Unlock()
}

// AddGauge sets a gauge value (replaces).
func (r *Registry) AddGauge(name string, value uint64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.metrics[name] = &series{def: Metric{Name: name, Type: Gauge, Unit: "count", Help: "Gauge: " + name}, value: value}
	r.mu.Unlock()
}

// Observe records a histogram sample (bounded by SampleLimit).
func (r *Registry) Observe(name string, ms uint64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.seriesLocked(name, nil)
	if len(s.samples) >= SampleLimit {
		// drop oldest; bounded storage (Gate §14)
		copy(s.samples, s.samples[1:])
		s.samples = s.samples[:SampleLimit-1]
	}
	s.samples = append(s.samples, ms)
}

// EmitToolDuration records a tool invocation (D2/D4): duration + failure by
// stable tool category. Tool names are bounded to tool names.
func EmitToolDuration(category string, durationMS int64, failed bool) {
	reg := DefaultRegistry()
	name := "tool.duration_ms"
	if category != "" {
		name = "tool.duration_ms[" + category + "]"
	}
	reg.Observe(name, uint64(maxInt64(durationMS, 0)))
	if failed {
		reg.Inc("tool.failures", map[string]string{"category": category}, 1)
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Snapshot returns the current series (bounded, documented).
func (r *Registry) Snapshot() map[string]MetricValue {
	out := map[string]MetricValue{}
	if r == nil {
		return out
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, s := range r.metrics {
		mv := MetricValue{Name: s.def.Name, Type: s.def.Type.String(), Unit: s.def.Unit, Help: s.def.Help, Labels: s.labels, Value: s.value}
		if len(s.samples) > 0 {
			mv.Percentiles = percentiles(s.samples)
		}
		out[name] = mv
	}
	return out
}

func percentiles(s []uint64) map[string]uint64 {
	if len(s) == 0 {
		return nil
	}
	sorted := append([]uint64(nil), s...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	pick := func(p float64) uint64 {
		idx := int(float64(len(sorted)-1) * p)
		if idx > len(sorted)-1 {
			idx = len(sorted) - 1
		}
		return sorted[idx]
	}
	return map[string]uint64{"p50": pick(0.50), "p90": pick(0.90), "p95": pick(0.95), "p99": pick(0.99), "max": sorted[len(sorted)-1]}
}

func (r *Registry) String() string {
	out := ""
	for _, mv := range r.Snapshot() {
		out += fmt.Sprintf("%s=%d [%s, %s]\n", mv.Name, mv.Value, mv.Type, mv.Unit)
	}
	return out
}
