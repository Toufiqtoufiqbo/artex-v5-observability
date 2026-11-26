package server

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/traffic"
)

// trafficEvidenceVerifier replaces "the fact says it saw a response" with
// "the process recorded that response". It is injected into every task agent
// that can call prove_goal.
type trafficEvidenceVerifier struct {
	traffic *traffic.Traffic
	pg      *db.DB
}

// goalEvidenceVerifier is nil only when the host has no activity ledger at all.
// The ledger is the default corroboration source: traffic capture may be off
// (or a fact may come from command output), and goal proof must still be checked
// against something the process recorded rather than the claim's shape alone.
func (s *Server) goalEvidenceVerifier() agent.EvidenceVerifier {
	if s == nil || s.m == nil || s.m.pg == nil {
		return nil
	}
	v := &trafficEvidenceVerifier{pg: s.m.pg}
	if s.m.traffic != nil && s.m.TrafficEnabled() {
		v.traffic = s.m.traffic
	}
	return v
}

var (
	claimRequestRE = regexp.MustCompile(`(?i)\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+([^\s"'<>]+)`)
	claimPathRE    = regexp.MustCompile(`/(?:[A-Za-z_][A-Za-z0-9._~%${}\-/]*)`)
	claimStatusRE  = regexp.MustCompile(`(?i)\bHTTP/\d(?:\.\d)?\s*(\d{3})\b`)
	claimStatusRaw = regexp.MustCompile(`(?i)\bHTTP/\d(?:\.\d)?\s*\d{3}\b`)
	claimBytesRE   = regexp.MustCompile(`(?i)\b\d{2,}\s*(?:bytes|byte|records|rows)\b`)
	claimTokenRE   = regexp.MustCompile(`[A-Za-z0-9_.@:/\-]{12,}`)

	// The authoritative request/status come from the first line of what
	// traffic.Get returned. A status code anywhere in the body is not a status.
	requestLineRE = regexp.MustCompile(`^([A-Z]+)[ \t]+(\S+)[ \t]+HTTP/\d(?:\.\d)?[ \t]*$`)
	statusLineRE  = regexp.MustCompile(`^HTTP(?:/\d(?:\.\d)?)?[ \t]+(\d{3})(?:[ \t]|$)`)
)

func taskExplorationID(ctx context.Context, pg *db.DB, taskID int64) (int64, error) {
	if pg == nil {
		return 0, fmt.Errorf("activity ledger unavailable")
	}
	var expID int64
	if err := pg.QueryRowContext(ctx, `SELECT exploration_id FROM tasks WHERE id=$1`, taskID).Scan(&expID); err != nil {
		return 0, fmt.Errorf("task %d lookup failed: %v", taskID, err)
	}
	return expID, nil
}

func (v *trafficEvidenceVerifier) VerifyFact(ctx context.Context, taskID int64, node *db.Node) error {
	if v == nil || node == nil {
		return fmt.Errorf("fact is missing")
	}
	claim := agent.FactEvidenceText(node)
	if strings.TrimSpace(claim) == "" {
		return fmt.Errorf("fact has no text to verify")
	}

	// Preferred path: the fact cites real traffic IDs (returned by
	// traffic_search / traffic_get). The exchange must exist, must have been
	// seen in THIS task's activity, and must carry the claimed request/status.
	if ids := factTrafficIDs(node); len(ids) > 0 {
		if v.traffic == nil {
			return fmt.Errorf("cited traffic but no recording backend is available")
		}
		expID, err := taskExplorationID(ctx, v.pg, taskID)
		if err != nil {
			return err
		}
		for _, id := range ids {
			req, resp, err := v.traffic.Get(id)
			if err != nil {
				return fmt.Errorf("cited traffic %q is not a recorded exchange", id)
			}
			local, err := v.pg.Exploration(expID).ActivityCitesTrafficID(ctx, id)
			if err != nil {
				return fmt.Errorf("task-local traffic check failed: %v", err)
			}
			if !local {
				return fmt.Errorf("cited traffic %q was not used in this task", id)
			}
			if !claimMatchesExchange(claim, req, resp) {
				return fmt.Errorf("cited traffic %q does not contain the claimed request/status", id)
			}
		}
		return nil
	}

	// Fallback for HTTPRequest / command tools, whose results live in the
	// task-scoped activity ledger rather than the recording proxy: two distinct
	// anchors must appear in ONE successful tool result. A single anchor can be
	// copied from an unrelated response and is not proof.
	anchors := factAnchors(claim)
	if len(anchors) < 2 {
		return fmt.Errorf("claim needs at least two concrete anchors in one tool result (request path, HTTP status line, byte delta or long tool-output token)")
	}
	expID, err := taskExplorationID(ctx, v.pg, taskID)
	if err != nil {
		return err
	}
	ok, err := v.pg.Exploration(expID).HasToolResultAnchor(ctx, anchors, 2)
	if err != nil {
		return fmt.Errorf("activity check failed: %v", err)
	}
	if !ok {
		return fmt.Errorf("no successful tool result in this task contains the claimed artifact (cite traffic_refs or quote the real request/status)")
	}
	return nil
}

// factTrafficIDs accepts both the new structured field and a legacy single id.
func factTrafficIDs(n *db.Node) []string {
	if n == nil || len(n.Payload) == 0 {
		return nil
	}
	var p map[string]json.RawMessage
	if err := json.Unmarshal(n.Payload, &p); err != nil {
		return nil
	}
	var ids []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			ids = append(ids, s)
			seen[s] = true
		}
	}
	if raw, ok := p["traffic_refs"]; ok {
		var refs []struct {
			TrafficID string `json:"traffic_id"`
		}
		if json.Unmarshal(raw, &refs) == nil {
			for _, r := range refs {
				add(r.TrafficID)
			}
		}
	}
	if raw, ok := p["traffic_ref"]; ok {
		var one string
		if json.Unmarshal(raw, &one) == nil {
			add(one)
		}
	}
	return ids
}

func firstHTTPLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func stripQuery(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}

// parseRequestLine parses the request line of an actual recorded exchange.
func parseRequestLine(line string) (method, path string, ok bool) {
	m := requestLineRE.FindStringSubmatch(strings.TrimSpace(line))
	if len(m) != 3 {
		return "", "", false
	}
	p := stripQuery(m[2])
	if p == "" {
		return "", "", false
	}
	return strings.ToUpper(m[1]), p, true
}

// parseStatusLine parses the status line (never the body) of an actual exchange.
func parseStatusLine(line string) (string, bool) {
	m := statusLineRE.FindStringSubmatch(strings.TrimSpace(line))
	if len(m) != 2 {
		return "", false
	}
	return m[1], true
}

// claimMatchesExchange compares the claim's request line and status against the
// parsed first lines of the cited exchange. A distinctive token is accepted only
// when the claim has no structured request/status to check.
func claimMatchesExchange(claim, req, resp string) bool {
	actualMethod, actualPath, haveRequest := parseRequestLine(firstHTTPLine(req))
	actualStatus, haveStatus := parseStatusLine(firstHTTPLine(resp))

	checked := 0
	if m := claimRequestRE.FindStringSubmatch(claim); len(m) == 3 {
		claimPath := stripQuery(m[2])
		if claimPath != "" {
			checked++
			if !haveRequest || strings.ToUpper(m[1]) != actualMethod || claimPath != actualPath {
				return false
			}
		}
	} else if m := claimPathRE.FindString(claim); m != "" {
		claimPath := stripQuery(m)
		checked++
		if !haveRequest || claimPath != actualPath {
			return false
		}
	}
	if m := claimStatusRE.FindStringSubmatch(claim); len(m) == 2 {
		checked++
		if !haveStatus || m[1] != actualStatus {
			return false
		}
	}
	if checked > 0 {
		return true
	}

	// Command-output style claims have no request/status to parse: require a
	// long token with both letters and digits that really occurs in the cited
	// exchange. Structured claims never fall back into the body.
	for _, m := range claimTokenRE.FindAllString(strings.ToLower(claim), -1) {
		if len(m) < 14 {
			continue
		}
		hasDigit := strings.IndexFunc(m, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
		hasLetter := strings.IndexFunc(m, func(r rune) bool { return r >= 'a' && r <= 'z' }) >= 0
		if !hasDigit || !hasLetter {
			continue
		}
		if strings.Contains(strings.ToLower(req), m) || strings.Contains(strings.ToLower(resp), m) {
			return true
		}
	}
	return false
}

func factAnchors(claim string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len(s) >= 5 && !seen[strings.ToLower(s)] {
			seen[strings.ToLower(s)] = true
			out = append(out, s)
		}
	}
	for _, m := range claimStatusRaw.FindAllString(claim, -1) {
		add(m)
	}
	for _, m := range claimBytesRE.FindAllString(claim, -1) {
		add(m)
	}
	if m := claimRequestRE.FindStringSubmatch(claim); len(m) == 3 && len(m[2]) >= 5 {
		add(stripQuery(m[2]))
	}
	if len(out) < 2 {
		for _, m := range claimPathRE.FindAllString(claim, -1) {
			add(m)
		}
	}
	if len(out) < 2 {
		for _, m := range claimTokenRE.FindAllString(claim, -1) {
			hasDigit := strings.IndexFunc(m, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
			if hasDigit || strings.ContainsAny(m, "@/-") {
				add(m)
			}
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}
