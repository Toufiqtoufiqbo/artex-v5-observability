package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	actool "github.com/Autumn-27/norma/tool"
)

// ---------------------------------------------------------------------------
// Retest — regression re-verification
//
// Phase 6 deliverable. Takes the findings the engine reports (same shape the
// agent passes to report_finding: id/name/class/url/param/method), re-runs the
// SAME oracle on each, and returns CONFIRMED / NOT CONFIRMED per finding plus
// the measurements. It is deliberately mechanical: no agent judgement — a
// finding is CONFIRMED iff the oracle re-measures signal.
// ---------------------------------------------------------------------------

type rtFinding struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Class      string `json:"class"`
	URL        string `json:"url"`
	ReadURL    string `json:"read_url"`
	Param      string `json:"param"`
	Method     string `json:"method"`
	BodyFormat string `json:"body_format"`
}

func newRetestTool() actool.CoreTool {
	schema := obj(map[string]any{
		"findings": map[string]any{
			"type":        "array",
			"description": "Findings to re-verify: id, name, class, url, param, method, body_format, and read_url for xss_stored. Class should be one of sqli_boolean, sqli_time, xss_reflect, xss_stored, ssti_eval, path_traversal (the same kind that produced the finding).",
			"items": obj(map[string]any{
				"id":          idp("finding id"),
				"name":        str("finding name"),
				"class":       str("vuln class / oracle kind"),
				"url":         str("endpoint URL (same as original finding)"),
				"read_url":    str("xss_stored only: endpoint that renders the persisted value"),
				"param":       str("parameter to inject (same as original finding)"),
				"method":      str("HTTP method (default GET)"),
				"body_format": map[string]any{"type": "string", "enum": []string{"form", "json"}, "description": "POST body format (default form)"},
			}),
		},
	}, "findings")

	desc := "Re-run the same oracle on a set of findings and report CONFIRMED/NOT CONFIRMED per finding with the re-measured values. Use it for retest after fixes: feed the findings you want to verify, and compare the verdicts + measurements against the original findings."
	return writeTool("Retest", desc, schema, runRetest)
}

func runOracleFor(ctx context.Context, t probeTarget, kind string) (actool.Result, error) {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "sqli_boolean":
		return oracleSQLiBoolean(ctx, t)
	case "sqli_time":
		return oracleSQLiTime(ctx, t)
	case "xss_reflect":
		return oracleXSSReflect(ctx, t)
	case "xss_stored":
		return oracleXSSStored(ctx, t)
	case "ssti_eval":
		return oracleSSTI(ctx, t)
	case "path_traversal":
		return oracleTraversal(ctx, t)
	default:
		return actool.Errorf(fmt.Sprintf("Retest: unknown class %q (use sqli_boolean, sqli_time, xss_reflect, xss_stored, ssti_eval, path_traversal)", kind)), nil
	}
}

func runRetest(ctx context.Context, in json.RawMessage) (actool.Result, error) {
	var a struct {
		Findings []rtFinding `json:"findings"`
	}
	if err := json.Unmarshal(in, &a); err != nil {
		return actool.Errorf("Retest: " + err.Error()), nil
	}
	if len(a.Findings) == 0 {
		return actool.Errorf("Retest: findings is empty"), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "retest pass: %d findings\n", len(a.Findings))
	for _, f := range a.Findings {
		method := strings.ToUpper(strings.TrimSpace(f.Method))
		if method == "" {
			method = "GET"
		}
		t := probeTarget{
			URL: f.URL, ReadURL: f.ReadURL, Param: f.Param, Method: method, BodyFormat: f.BodyFormat,
			Timeout:  15 * time.Second,
			Insecure: true,
		}
		res, err := runOracleFor(ctx, t, f.Class)
		name := f.Name
		if name == "" {
			name = fmt.Sprintf("finding#%d", f.ID)
		}
		if err != nil {
			fmt.Fprintf(&b, "\nretest id=%d name=%q class=%s url=%s param=%s -> verdict=ERROR (%v)", f.ID, name, f.Class, f.URL, f.Param, err)
			continue
		}
		out := res.Flatten()
		verdict := "UNKNOWN"
		if strings.Contains(out, "VERDICT: CONFIRMED") {
			verdict = "CONFIRMED"
		} else if strings.Contains(out, "VERDICT: NOT CONFIRMED") {
			verdict = "NOT CONFIRMED"
		}
		fmt.Fprintf(&b, "\nretest id=%d name=%q class=%s url=%s param=%s -> verdict=%s\n", f.ID, name, f.Class, f.URL, f.Param, verdict)
		// Print up to 3 measurement/evidence lines.
		n := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "VERDICT") || strings.Contains(line, "distinct_responses") || strings.HasPrefix(line, "evidence") {
				fmt.Fprintf(&b, "  %s\n", line)
				n++
				if n >= 3 {
					break
				}
			}
		}
	}
	return actool.Text(b.String()), nil
}
