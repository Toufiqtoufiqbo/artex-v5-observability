package evidence

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/traffic"
)

// A finding can carry a reproducible HTTP artifact in its evidence text without
// the reporting agent ever calling traffic_search (observed in autonomous runs:
// 100 captured exchanges, three findings, zero bindings). When binding is
// enabled we derive conservative references from the text the agent itself
// wrote, then verify them against the recorded index before attaching them.
//
// Matching is deliberately exact on three dimensions — method, URL path and
// status code — so we never bind by host or timestamp alone. Ambiguous or
// unparseable evidence yields no binding, not a guessed one.
var (
	evidenceRequestLineRe = regexp.MustCompile(`(?im)\b(GET|POST|PUT|PATCH|DELETE|HEAD)\s+(?:https?://[^\s/]+)?(/[^\s"'<>]*)`)
	evidenceStatusRe      = regexp.MustCompile(`(?i)(?:HTTP(?:/[12]\.\d)?\s+|\bstatus\s*[:=]\s*)([1-5][0-9]{2})\b`)
)

type evidenceProbe struct {
	Method string
	Path   string
	Status string
}

// probesFromEvidence extracts distinct (method, path, status) triples from a
// finding's own evidence text. It is pure so it can be unit tested without a DB.
func probesFromEvidence(text string) []evidenceProbe {
	lines := evidenceRequestLineRe.FindAllStringSubmatchIndex(text, -1)
	statuses := evidenceStatusRe.FindAllStringSubmatchIndex(text, -1)
	if len(lines) == 0 || len(statuses) == 0 {
		return nil
	}
	// Evidence often carries a baseline and an injection pair. Bind each request
	// line to the first status code that follows it; only fall back to the first
	// status in the text when the request line is the last thing written.
	seen := map[string]bool{}
	var out []evidenceProbe
	for _, line := range lines {
		method := strings.ToUpper(text[line[2]:line[3]])
		path := strings.TrimSpace(text[line[4]:line[5]])
		if i := strings.IndexByte(path, '?'); i >= 0 {
			path = path[:i]
		}
		if path == "" || path == "/" {
			continue
		}
		status := text[statuses[0][2]:statuses[0][3]]
		for _, st := range statuses {
			if st[0] > line[1] {
				status = text[st[2]:st[3]]
				break
			}
		}
		key := method + " " + path + " " + status
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, evidenceProbe{Method: method, Path: path, Status: status})
		if len(out) >= 3 {
			break
		}
	}
	return out
}

// autoBindRefs resolves the evidence's own request lines against the recorded
// traffic index. Every candidate must match method, exact URL path and status;
// at most one exchange per probe is attached, newest first.
func (s *Store) autoBindRefs(ctx context.Context, in db.RecordFindingInput) []db.TrafficRef {
	if s.Traffic == nil {
		return nil
	}
	probes := probesFromEvidence(in.Evidence + "\n" + in.Summary)
	if len(probes) == 0 {
		return nil
	}
	var refs []db.TrafficRef
	used := map[string]bool{}
	for _, p := range probes {
		if err := ctx.Err(); err != nil {
			return refs
		}
		f := traffic.PageQuery{
			Method: p.Method, Path: p.Path, Status: p.Status,
			RespMin: -1, RespMax: -1, Sort: "ts", Order: "desc",
		}
		rows, _, err := s.Traffic.Page(f, 0, 3)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if used[row.ID] {
				continue
			}
			u, err := url.Parse(row.URL)
			if err != nil || u.Path != p.Path {
				continue
			}
			used[row.ID] = true
			refs = append(refs, db.TrafficRef{
				TrafficID: row.ID,
				Role:      "proof",
				Note:      "自动关联：证据中的请求行与状态码精确匹配此条录制流量",
			})
			break
		}
		if len(refs) >= 3 {
			break
		}
	}
	return refs
}
