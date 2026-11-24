package telemetry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Config (Gate B4).
type Config struct {
	Enabled bool
	Dir     string // telemetry events dir; empty = in-memory only
	// MaxFileSize rotates events to a new file when exceeded.
	MaxFileSize int64
}

// Recorder appends events to a bounded append-only JSONL store and keeps
// aggregate metrics. It is safe for concurrent use, never blocks execution and
// must never change control flow when disabled.
type Recorder struct {
	cfg Config
	mu  sync.Mutex // serialize writes
	f   *os.File
	w   *bufio.Writer
	bw  bool // events-dir mode (w != nil)
	n   writeFailures
	eventsInStore int64
}

type writeFailures struct{ v atomic.Uint64 }

func (w *writeFailures) add() { w.v.Add(1) }
func (w *writeFailures) load() uint64 { return w.v.Load() }

// NewRecorder returns a Recorder; when cfg.Dir != "" it opens/appends events-<ts>.jsonl.
func NewRecorder(cfg Config) *Recorder {
	r := &Recorder{cfg: cfg}
	if cfg.Enabled && cfg.Dir != "" {
		if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
			r.n.add()
		} else if f, err := os.OpenFile(filepath.Join(cfg.Dir, EventsFileName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			r.f, r.w, r.bw = f, bufio.NewWriter(f), true
		} else {
			r.n.add()
		}
	}
	return r
}

const EventsFileName = "events.jsonl"

// maxRotatedFiles bounds total telemetry disk usage.
const maxRotatedFiles = 3

// defaultMaxFileSize bounds one event file before rotation.
const defaultMaxFileSize int64 = 50 * 1024 * 1024

// Enabled reports whether the recorder stores events (Gate B4; nil/false = no-op).
func (r *Recorder) Enabled() bool { return r != nil && r.cfg.Enabled }

// WriteFailures returns the count of telemetry store write failures (Gate B4).
func (r *Recorder) WriteFailures() uint64 {
	if r == nil {
		return 0
	}
	return r.n.load()
}

// Append appends one event to the store. Telemetry-only; errors are counted.
func (r *Recorder) Append(ev *Event) {
	if r == nil || !r.Enabled() || ev == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w == nil {
		return
	}
	// Rotation: bound disk usage. Failures here are counted but never affect execution.
	if err := r.rotateIfNeeded(); err != nil {
		r.n.add()
	}
	line, err := ev.MarshalJSON()
	if err != nil {
		r.n.add()
		return
	}
	if _, err := r.w.Write(append(line, '\n')); err != nil {
		r.n.add()
		return
	}
	if err := r.w.Flush(); err != nil {
		r.n.add()
		return
	}
	atomic.AddInt64(&r.eventsInStore, 1)
}

// rotateIfNeeded rotates events.jsonl when it exceeds MaxFileSize and prunes
// the oldest rotated files, keeping at most maxRotatedFiles.
func (r *Recorder) rotateIfNeeded() error {
	if r.w == nil || r.f == nil {
		return nil
	}
	limit := r.cfg.MaxFileSize
	if limit <= 0 {
		limit = defaultMaxFileSize
	}
	st, err := r.f.Stat()
	if err != nil {
		return err
	}
	if st.Size() < limit {
		return nil
	}
	if err := r.w.Flush(); err != nil {
		return err
	}
	if err := r.f.Close(); err != nil {
		return err
	}
	rotated := filepath.Join(r.cfg.Dir, fmt.Sprintf("events-%s.jsonl", time.Now().UTC().Format("20060102T150405.000000000Z")))
	if err := os.Rename(filepath.Join(r.cfg.Dir, EventsFileName), rotated); err != nil {
		return err
	}
	// Prune oldest rotated files (bounded disk).
	matches, _ := filepath.Glob(filepath.Join(r.cfg.Dir, "events-*.jsonl"))
	sort.Strings(matches)
	for len(matches) > maxRotatedFiles {
		_ = os.Remove(matches[0])
		matches = matches[1:]
	}
	f, err := os.OpenFile(filepath.Join(r.cfg.Dir, EventsFileName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	r.f, r.w = f, bufio.NewWriter(f)
	return nil
}

// EventsInStore returns the number of events appended in this session.
func (r *Recorder) EventsInStore() int64 {
	if r == nil {
		return 0
	}
	return atomic.LoadInt64(&r.eventsInStore)
}

// Close flushes and closes the store.
func (r *Recorder) Close() error {
	if r == nil || r.w == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w == nil {
		return nil
	}
	_ = r.w.Flush()
	err := r.f.Close()
	r.w = nil
	return err
}

// ReadEvents parses the JSONL store. Missing files are not an error (Gate B4:
// nothing written yet is not a telemetry failure).
func ReadEvents(dir string) ([]*Event, error) {
	// Read the active file and all rotated files (chronological by name).
	files, _ := filepath.Glob(filepath.Join(dir, "events*.jsonl"))
	sort.Strings(files)
	var out []*Event
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var ev Event
			if err := json.Unmarshal(line, &ev); err != nil {
				continue // skip malformed lines, never fail reads (Gate B4)
			}
			out = append(out, &ev)
		}
		scanErr := sc.Err()
		_ = f.Close()
		if scanErr != nil {
			return out, scanErr
		}
	}
	SortEvents(out)
	return out, nil
}

// SnapshotToFile writes an aggregated metrics snapshot to dir.
func SnapshotToFile(r *Registry, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "metrics.json")
	f, err := os.Create(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r.Snapshot()); err != nil {
		return "", err
	}
	return p, nil
}

// StampStartedAt is a helper used by instrumentation call sites.
func StampStartedAt() time.Time { return time.Now().UTC() }
