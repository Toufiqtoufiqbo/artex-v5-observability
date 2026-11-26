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

// goalEvidenceVerifier is nil when the host has no recording backend; in that
// case the gate keeps its older shape-only behaviour rather than blocking all
// goal proofs.
func (s *Server) goalEvidenceVerifier() agent.EvidenceVerifier {
	if s == nil || s.m == nil || s.m.traffic == nil || !s.m.TrafficEnabled() {
		return nil
	}
	return &trafficEvidenceVerifier{traffic: s.m.traffic, pg: s.m.pg}
}

var (
	claimRequestRE = regexp.MustCompile(`(?i)\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+([^\s"'<>]+)`)
	claimPathRE    = regexp.MustCompile(`/(?:[A-Za-z_][A-Za-z0-9._~%${}\-/]*)`)
	claimStatusRE  = regexp.MustCompile(`(?i)\bHTTP/\d(?:\.\d)?\s*(\d{3})\b`)
	claimStatusRaw = regexp.MustCompile(`(?i)\bHTTP/\d(?:\.\d)?\s*\d{3}\b`)
	claimBytesRE   = regexp.MustCompile(`(?i)\b\d{2,}\s*(?:bytes|byte|records|rows)\b`)
	claimTokenRE   = regexp.MustCompile(`[A-Za-z0-9_.@:/\-]{12,}`)
)

func (v *trafficEvidenceVerifier) VerifyFact(ctx context.Context, taskID int64, node *db.Node) error {
	if v == nil || node == nil {
		return fmt.Errorf("fact is missing")
	}
	claim := agent.FactEvidenceText(node)
	if strings.TrimSpace(claim) == "" {
		return fmt.Errorf("fact has no text to verify")
	}

	// Preferred path: the fact cites real traffic IDs (returned by
	// traffic_search / traffic_get). Check the exchange exists and actually
	// carries the claimed request/status/token.
	if ids := factTrafficIDs(node); len(ids) > 0 {
		if v.traffic == nil {
			return fmt.Errorf("cited traffic but no recording backend is available")
		}
		for _, id := range ids {
			req, resp, err := v.traffic.Get(id)
			if err != nil {
				return fmt.Errorf("cited traffic %q is not a recorded exchange", id)
			}
			if !claimMatchesExchange(claim, req, resp) {
				return fmt.Errorf("cited traffic %q does not contain the claimed request/status", id)
			}
		}
		return nil
	}

	// Fallback for HTTPRequest / command tools, whose results live in the
	// task-scoped activity ledger rather than the recording proxy: at least
	// two independent anchors must appear in ONE successful tool result.
	anchors := factAnchors(claim)
	if len(anchors) == 0 {
		return fmt.Errorf("claim has no concrete anchor (request path, HTTP status line, byte delta or long tool-output token)")
	}
	min := 2
	if len(anchors) == 1 {
		min = 1
	}
	if v.pg == nil {
		return fmt.Errorf("activity ledger unavailable")
	}
	var expID int64
	if err := v.pg.QueryRowContext(ctx, `SELECT exploration_id FROM tasks WHERE id=$1`, taskID).Scan(&expID); err != nil {
		return fmt.Errorf("task %d lookup failed: %v", taskID, err)
	}
	ok, err := v.pg.Exploration(expID).HasToolResultAnchor(ctx, anchors, min)
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

// claimMatchesExchange requires the claimed request path AND status to agree
// with the cited exchange when both are stated; a single distinctive token is
// enough for command-output style claims.
func claimMatchesExchange(claim, req, resp string) bool {
	reqLower, respLower, claimLower := strings.ToLower(req), strings.ToLower(resp), strings.ToLower(claim)
	matched, checked := true, 0
	if m := claimRequestRE.FindStringSubmatch(claim); len(m) == 3 {
		p := strings.ToLower(m[2])
		if i := strings.IndexByte(p, '?'); i >= 0 {
			p = p[:i]
		}
		if len(p) >= 4 {
			checked++
			if !strings.Contains(reqLower, p) {
				matched = false
			}
		}
	} else if m := claimPathRE.FindString(claim); m != "" && len(m) >= 5 {
		checked++
		if !strings.Contains(reqLower, strings.ToLower(m)) {
			matched = false
		}
	}
	if m := claimStatusRE.FindStringSubmatch(claim); len(m) == 2 {
		checked++
		code := m[1]
		if !strings.Contains(respLower, code) && !strings.Contains(reqLower, code) {
			matched = false
		}
	}
	if checked >= 2 {
		return matched
	}
	for _, m := range claimTokenRE.FindAllString(claimLower, -1) {
		hasDigit := strings.IndexFunc(m, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
		if len(m) < 12 || (!hasDigit && !strings.ContainsAny(m, "@/-")) {
			continue
		}
		if strings.Contains(reqLower, m) || strings.Contains(respLower, m) {
			return true
		}
	}
	return checked > 0 && matched
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
		path := m[2]
		if i := strings.IndexByte(path, '?'); i >= 0 {
			path = path[:i]
		}
		add(path)
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
