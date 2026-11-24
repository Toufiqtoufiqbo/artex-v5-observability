package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/Autumn-27/artex/telemetry"
)

// diagnostics.go — Phase IX.2.3.1 (Gate I2): diagnostics API over the
// IX.2.3 telemetry layer. Endpoints reuse existing auth: they are plain
// GET handlers on the same mux and expose only aggregated metrics + event
// metadata (§G3).

// s.diagnosticsSummary — GET /api/diagnostics/summary
func (s *Server) diagnosticsSummary(w http.ResponseWriter, r *http.Request) {
	reg := telemetry.DefaultRegistry()
	snap := reg.Snapshot()
	// Count events if a recorder exists (read-only).
	var events int
	if rec := telemetry.DefaultRecorder(); rec != nil {
		events = int(rec.EventsInStore())
	}
	writeJSON(w, 200, map[string]any{
		"metrics": snap,
		"events_total": events,
		"write_failures": telemetry.DefaultRecorder().WriteFailures(),
	})
}

// s.diagnosticsRun — GET /api/diagnostics/run?run_id=<id>
func (s *Server) diagnosticsRun(w http.ResponseWriter, r *http.Request) {
	dir := telemetryDir()
	evs, err := telemetry.ReadEvents(dir)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	runID := r.URL.Query().Get("run_id")
	out := []*telemetry.Event{}
	for _, e := range evs {
		if runID == "" || e.RunID == runID {
			out = append(out, e)
		}
	}
	writeJSON(w, 200, out)
}

// s.diagnosticsTask — GET /api/diagnostics/task?task_id=<id>
func (s *Server) diagnosticsTask(w http.ResponseWriter, r *http.Request) {
	dir := telemetryDir()
	evs, err := telemetry.ReadEvents(dir)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	taskID := r.URL.Query().Get("task_id")
	out := []*telemetry.Event{}
	for _, e := range evs {
		if taskID != "" && e.TaskID == taskID {
			out = append(out, e)
		}
	}
	writeJSON(w, 200, out)
}

// s.diagnosticsTimeline — GET /api/diagnostics/timeline?task_id=<id>
func (s *Server) diagnosticsTimeline(w http.ResponseWriter, r *http.Request) {
	dir := telemetryDir()
	evs, err := telemetry.ReadEvents(dir)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeErr(w, 400, "task_id required")
		return
	}
	writeJSON(w, 200, telemetry.BuildTimeline(evs, taskID))
}

func telemetryDir() string {
	if d := strings.TrimSpace(os.Getenv("ARTEX_TELEMETRY_DIR")); d != "" {
		return d
	}
	return "data/telemetry"
}
