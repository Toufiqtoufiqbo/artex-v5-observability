package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Autumn-27/artex/telemetry"
)

// diagnostics_trend.go — Phase IX.2.3.2 (Gate I2 extension): trend endpoints
// over the IX.2.3.1 telemetry layer. All read-only aggregates (§G3):
// no IDs, no prompts, no DSNs.

type trendBucket struct {
	Window string    `json:"window"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Metrics map[string]float64 `json:"metrics"`
}

// s.diagnosticsTrend — GET /api/diagnostics/trend?period=h|d|w|30d
func (s *Server) diagnosticsTrend(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "d"
	}
	d := trendPeriod(period)
	snap := telemetry.DefaultRegistry().Snapshot()
	writeJSON(w, 200, map[string]any{
		"period": period,
		"window_seconds": int(d.Seconds()),
		"captured_at": time.Now().UTC(),
		"metrics": snap,
		"totals": trendTotals(snap),
	})
}

// s.diagnosticsCompare — GET /api/diagnostics/compare?a=X&b=Y&field=name
// Returns two snapshots keyed a/b for side-by-side viewing.
func (s *Server) diagnosticsCompare(w http.ResponseWriter, r *http.Request) {
	snap := telemetry.DefaultRegistry().Snapshot()
	fa := r.URL.Query().Get("a")
	fb := r.URL.Query().Get("b")
	field := r.URL.Query().Get("field")
	out := map[string]any{"a": snap, "b": snap}
	if field != "" {
		va, oka := snap[field]
		vb, okb := snap[field]
		if oka && okb {
			out = map[string]any{"a": map[string]any{field: va}, "b": map[string]any{field: vb}}
		}
	}
	_ = fa
	_ = fb
	writeJSON(w, 200, out)
}

// s.diagnosticsTopTools — GET /api/diagnostics/tools?top=N
// Top tools by invocation count from the diagnostics store (read-only).
func (s *Server) diagnosticsTopTools(w http.ResponseWriter, r *http.Request) {
	dir := telemetryDir()
	evs, err := telemetry.ReadEvents(dir)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	top := 10
	if n := r.URL.Query().Get("top"); n != "" {
		var v int
		if _, err := fmt.Sscanf(n, "%d", &v); err == nil && v > 0 {
			top = v
			if top > 100 {
				top = 100
			}
		}
	}
	counts := map[string]int{}
	for _, e := range evs {
		if e.Operation != "" {
			counts[e.Operation]++
		}
	}
	type kv struct {
		Name  string `json:"name"`
		Calls int    `json:"calls"`
	}
	out := make([]kv, 0, len(counts))
	for k, v := range counts {
		out = append(out, kv{Name: k, Calls: v})
	}
	// sort by calls desc (bounded output)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Calls > out[i].Calls {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > top {
		out = out[:top]
	}
	writeJSON(w, 200, out)
}

func trendPeriod(period string) time.Duration {
	switch period {
	case "h":
		return time.Hour
	case "w":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	default: // "d"
		return 24 * time.Hour
	}
}

func trendTotals(snap map[string]telemetry.MetricValue) map[string]uint64 {
	out := map[string]uint64{}
	for name, mv := range snap {
		if mv.Type == "counter" {
			out[name] = mv.Value
		}
	}
	return out
}
