package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
)

// ---------------------------------------------------------------------------
// AttackPath — connecting confirmed findings into prerequisites chains
//
// Purpose: after the engine has a bag of confirmed findings, ask "what if
// finding A happens first — how much does finding B become exploitable?" The
// graph: nodes = findings, edges = hard prerequisites (shared asset,
// identity→target). Paths are output as chains.
//
// Deliberately conservative: it does NOT invent an exploit chain by default.
// It proposes edges from measurable facts (shared asset, identity finding on
// a host) and prints chains for the agent to evaluate.
// ---------------------------------------------------------------------------

type apFinding struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Class    string  `json:"class"`
	Severity string  `json:"severity"`
	URL      string  `json:"url"`
	Host     string  `json:"host"`
	Assets   []int64 `json:"assets"`
}

func apHostOf(f apFinding) string {
	if f.Host != "" {
		return f.Host
	}
	return strings.ToLower(mustHost(f.URL))
}

type apNode struct {
	F        int
	Name     string
	Class    string
	Severity string
	Host     string
	Assets   map[int]bool
}

type apPath struct {
	ids []int
}

func newAttackPathTool() actool.CoreTool {
	schema := obj(map[string]any{
		"findings": map[string]any{
			"type":        "array",
			"description": "The confirmed findings to connect. Fields: id, name, class, severity, url (or host), assets (array of ints).",
			"items":       apFindingSchema(),
		},
		"max_paths": intp("max paths to output (default 12)"),
	}, "findings")

	desc := "Connect confirmed findings into attack-path chains. Prerequisites: shared asset, identity (Login/session/auth class), or same host. Returns chains of finding names with severities and the connecting reason. It does not auto-execute chains — it surfaces them for the agent to evaluate."
	return writeTool("AttackPath", desc, schema, runAttackPath)
}

func apFindingSchema() map[string]any {
	return obj(map[string]any{
		"id":       idp("finding id"),
		"name":     str("finding name"),
		"class":    str("vuln class (sql_injection, idor, bola, xss, ssti, auth, ...)"),
		"severity": str("critical|high|medium|low|info"),
		"url":      str("target URL or endpoint (fallback when host is empty)"),
		"host":     str("optional: endpoint host (e.g. 127.0.0.1:3002)"),
		"assets":   map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
	})
}

func runAttackPath(ctx context.Context, in json.RawMessage) (actool.Result, error) {
	var a struct {
		Findings []apFinding `json:"findings"`
		MaxPaths int         `json:"max_paths"`
	}
	if err := json.Unmarshal(in, &a); err != nil {
		return actool.Errorf("AttackPath: " + err.Error()), nil
	}
	if len(a.Findings) == 0 {
		return actool.Errorf("AttackPath: findings is empty"), nil
	}
	maxPaths := a.MaxPaths
	if maxPaths <= 0 {
		maxPaths = 12
	}

	nodes := make([]apNode, len(a.Findings))
	adj := make([][]int, len(a.Findings))
	for i, f := range a.Findings {
		nodes[i] = apNode{F: i, Name: f.Name, Class: strings.ToLower(f.Class), Severity: strings.ToLower(f.Severity), Host: apHostOf(f)}
		for _, x := range f.Assets {
			if nodes[i].Assets == nil {
				nodes[i].Assets = map[int]bool{}
			}
			nodes[i].Assets[int(x)] = true
		}
	}
	addEdge := func(a, b int) {
		if a == b {
			return
		}
		for _, e := range adj[a] {
			if e == b {
				return
			}
		}
		adj[a] = append(adj[a], b)
		adj[b] = append(adj[b], a)
	}

	// 1) Shared asset.
	byAsset := map[int][]int{}
	for _, n := range nodes {
		for x := range n.Assets {
			byAsset[x] = append(byAsset[x], n.F)
		}
	}
	for _, g := range byAsset {
		for i := 0; i < len(g); i++ {
			for j := i + 1; j < len(g); j++ {
				addEdge(g[i], g[j])
			}
		}
	}
	// 2) Same host (informational connection between findings on one host).
	byHost := map[string][]int{}
	for _, n := range nodes {
		if n.Host == "" {
			continue
		}
		byHost[n.Host] = append(byHost[n.Host], n.F)
	}
	for _, g := range byHost {
		for i := 0; i < len(g); i++ {
			for j := i + 1; j < len(g); j++ {
				addEdge(g[i], g[j])
			}
		}
	}
	// 3) Identity → others (auth/login/session class unlocks the host).
	for _, n := range nodes {
		if strings.Contains(n.Class, "auth") || strings.Contains(n.Class, "login") || strings.Contains(n.Class, "session") {
			for m := range nodes {
				addEdge(n.F, m)
			}
		}
	}

	// 4) BFS from every node to enumerate chains of length >= 2.
	seen := map[string]bool{}
	var out []apPath
	for start := range nodes {
		type item struct {
			ids []int
		}
		q := []item{{ids: []int{start}}}
		for len(q) > 0 && len(out) < maxPaths {
			cur := q[0]
			q = q[1:]
			last := cur.ids[len(cur.ids)-1]
			if len(cur.ids) >= 2 {
				key := fmt.Sprint(cur.ids)
				if !seen[key] {
					seen[key] = true
					out = append(out, apPath{ids: append([]int{}, cur.ids...)})
				}
			}
			if len(cur.ids) >= 5 {
				continue
			}
			for _, e := range adj[last] {
				used := false
				for _, id := range cur.ids {
					if id == e {
						used = true
					}
				}
				if used {
					continue
				}
				q = append(q, item{ids: append(append([]int{}, cur.ids...), e)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i].ids) > len(out[j].ids) })

	var b strings.Builder
	fmt.Fprintf(&b, "Attack paths: %d chain(s) (searched over %d findings, max %d)\n", len(out), len(nodes), maxPaths)
	for i, p := range out {
		if i >= maxPaths {
			break
		}
		var parts []string
		for _, id := range p.ids {
			n := nodes[id]
			label := n.Name
			if label == "" {
				label = fmt.Sprintf("finding#%d", id+1)
			}
			parts = append(parts, fmt.Sprintf("%s (%s, %s)", label, n.Severity, n.Class))
		}
		fmt.Fprintf(&b, "\nChain: %s", strings.Join(parts, " -> "))
	}
	if len(out) == 0 {
		b.WriteString("\nNo chains found — findings are not connected by shared assets, identity, or host. Without an identity first (Login/session), authorization-escalation chains have no edge.")
	}
	return actool.Text(b.String()), nil
}
