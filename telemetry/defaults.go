package telemetry

import "sync"

// DefaultRegistry and DefaultRecorder are the process-wide telemetry instances.
// They are nil-safe no-ops by default (telemetry.Enabled=false).
var (
	defaultMu       sync.RWMutex
	defaultRegistry = NewRegistry()
	defaultRecorder *Recorder
)

// SetDefaultRecorder installs the process-wide recorder. nil disables.
func SetDefaultRecorder(r *Recorder) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRecorder = r
}

// SetDefaultRegistry installs the process-wide registry.
func SetDefaultRegistry(r *Registry) {
	if r == nil {
		r = NewRegistry()
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRegistry = r
}

// DefaultRegistry returns the process-wide registry.
func DefaultRegistry() *Registry {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	if defaultRegistry == nil {
		return NewRegistry()
	}
	return defaultRegistry
}

// DefaultRecorder returns the process-wide recorder (may be nil = disabled).
func DefaultRecorder() *Recorder {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultRecorder
}

// Emit appends an event to the default recorder if enabled. Never fails.
func Emit(ev *Event) {
	if rec := DefaultRecorder(); rec != nil {
		rec.Append(ev)
	}
}

// EmitMetric increments a metric on the default registry. Never fails.
func EmitMetric(name string, labels map[string]string, n uint64) {
	DefaultRegistry().Inc(name, labels, n)
}

// EmitHistogram observes a sample on the default registry.
func EmitHistogram(name string, ms uint64) {
	DefaultRegistry().Observe(name, ms)
}

// Enable turns on a file-backed recorder in dir and installs it as default.
func Enable(dir string) {
	rec := NewRecorder(Config{Enabled: true, Dir: dir})
	SetDefaultRecorder(rec)
}
