package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Autumn-27/artex/telemetry"
)

// cmdDiagnostics implements the `artex diagnostics` subcommands (Gate I4).
//
// Commands (candidate, still mirroring `benchmark` dispatch style):
//   artex diagnostics summary [-dir DIR]
//   artex diagnostics run      <run_id> [-dir DIR]
//   artex diagnostics task     <task_id> [-dir DIR]
//   artex diagnostics metrics  [-dir DIR]
func cmdDiagnostics(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("Usage:\n  artex diagnostics summary [-dir DIR]\n  artex diagnostics run <run_id> [-dir DIR]\n  artex diagnostics task <task_id> [-dir DIR]\n  artex diagnostics metrics [-dir DIR]")
		return 0
	}
	switch args[0] {
	case "summary":
		return cmdDiagnosticsSummary(args[1:])
	case "run":
		return cmdDiagnosticsFilter(args[1:], "run")
	case "task":
		return cmdDiagnosticsFilter(args[1:], "task")
	case "metrics":
		return cmdDiagnosticsMetrics(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown diagnostics command: %s\n", args[0])
		return 2
	}
}

func diagnosticsDir(def string) string {
	fs := flag.NewFlagSet("diagnostics", flag.ContinueOnError)
	dir := fs.String("dir", def, "telemetry events dir")
	_ = fs.Parse(os.Args[2:])
	if v := os.Getenv("ARTEX_TELEMETRY_DIR"); v != "" {
		*dir = v
	}
	return *dir
}

func cmdDiagnosticsSummary(args []string) int {
	dir := diagnosticsDir(defaultTelemetryDir())
	evs, err := telemetry.ReadEvents(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read events: %v\n", err)
		return 2
	}
	counts := map[string]int{}
	for _, e := range evs {
		counts[string(e.EventType)]++
	}
	out := map[string]any{
		"dir":           dir,
		"events_total":  len(evs),
		"by_type":       counts,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	return 0
}

func cmdDiagnosticsFilter(args []string, what string) int {
	id := ""
	for i, a := range args {
		if a == "-dir" {
			continue
		}
		if !strings.HasPrefix(a, "-") {
			id = a
			_ = i
		}
	}
	dir := diagnosticsDir(defaultTelemetryDir())
	evs, err := telemetry.ReadEvents(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read events: %v\n", err)
		return 2
	}
	key := "RunID"
	if what == "task" {
		key = "TaskID"
	}
	for _, e := range evs {
		var v string
		switch key {
		case "RunID":
			v = e.RunID
		case "TaskID":
			v = e.TaskID
		}
		if v == id || (id == "" && v != "") {
			b, _ := json.MarshalIndent(e, "", "  ")
			fmt.Println(string(b))
		}
	}
	return 0
}

func cmdDiagnosticsMetrics(args []string) int {
	dir := diagnosticsDir(defaultTelemetryDir())
	// Snapshot the process registry if enabled; otherwise read the metrics file.
	reg := telemetry.DefaultRegistry()
	snap := reg.Snapshot()
	if len(snap) == 0 {
		evs, err := telemetry.ReadEvents(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read events: %v\n", err)
			return 2
		}
		_ = evs
		fmt.Println("(no in-process metrics; run with telemetry enabled)")
	}
	b, _ := json.MarshalIndent(snap, "", "  ")
	fmt.Println(string(b))
	return 0
}

func defaultTelemetryDir() string {
	if v := os.Getenv("ARTEX_TELEMETRY_DIR"); v != "" {
		return v
	}
	return "data/telemetry"
}
