package benchmark

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// matcher.go — the matching rules that turn (expected, actual) finding sets into
// structured verdicts (Phase IX.1 D4). Matching is structural: a finding
// matches an expected entry when the normalized vuln_class matches (required)
// and every non-empty expected field matches (name/severity wildcards when
// empty). Ambiguity never resolves into a guess — unmatched findings and
// expected findings are reported as FP/FN.

// normalizeText lowercases and collapses whitespace.
func normalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Join(strings.Fields(s), " ")
}

// Fingerprint is the stable identity of an expected finding (normalized
// non-empty fields joined by NUL).
func (e ExpectedFinding) Fingerprint() string {
	parts := make([]string, 0, 3)
	for _, p := range []string{e.VulnClass, e.Name, e.Severity} {
		if p = normalizeText(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\x00")
}

// matches reports whether actual satisfies this expected finding: vuln_class
// must match, and every NON-EMPTY expected field must equal the actual field.
// Empty expected name/severity are wildcards (structural matching).
func (e ExpectedFinding) matches(a ActualFinding) bool {
	if e.VulnClass == "" || normalizeText(e.VulnClass) != normalizeText(a.VulnClass) {
		return false
	}
	if e.Name != "" && normalizeText(e.Name) != normalizeText(a.Name) {
		return false
	}
	if e.Severity != "" && normalizeText(e.Severity) != normalizeText(a.Severity) {
		return false
	}
	return true
}

// Key is the matching key: the override if set, else the structural fingerprint.
func (e ExpectedFinding) Key() string {
	if strings.TrimSpace(e.MatchKey) != "" {
		return strings.TrimSpace(e.MatchKey)
	}
	return e.Fingerprint()
}

// ActualFinding is one finding produced by the engine, as observed by the
// benchmark (from the findings table + graph nodes + evidence state).
type ActualFinding struct {
	Fingerprint     string `json:"fingerprint"`
	NodeID          int64  `json:"node_id"`
	FindingID       int64  `json:"finding_id"`
	VulnClass       string `json:"vuln_class"`
	Name            string `json:"name,omitempty"`
	Severity        string `json:"severity,omitempty"`
	Worker          string `json:"worker,omitempty"`
	EvidenceState   string `json:"evidence_state,omitempty"`
	EvidenceHasBody bool   `json:"evidence_has_body"`
}

// MatchResult is the deterministic confusion-matrix decomposition of one case.
type MatchResult struct {
	Matched           []FindingMatch    `json:"matched"`
	UnmatchedExpected []ExpectedFinding `json:"unmatched_expected"`
	UnmatchedActual   []ActualFinding   `json:"unmatched_actual"`
}

// FindingMatch pairs one ground-truth entry with the actual finding it matches.
type FindingMatch struct {
	Expected ExpectedFinding `json:"expected"`
	Actual   ActualFinding   `json:"actual"`
}

// MatchFindings computes the confusion-matrix decomposition (§D4). Actual
// findings are deduplicated by fingerprint first: a duplicate does not inflate
// TP and is not double-counted.
func MatchFindings(expected []ExpectedFinding, actual []ActualFinding) MatchResult {
	// Deduplicate actual findings by fingerprint.
	deduped := make([]ActualFinding, 0, len(actual))
	seen := map[string]bool{}
	for _, a := range actual {
		if a.Fingerprint == "" || seen[a.Fingerprint] {
			continue
		}
		seen[a.Fingerprint] = true
		deduped = append(deduped, a)
	}
	sort.Slice(deduped, func(i, j int) bool { return deduped[i].FindingID < deduped[j].FindingID })

	// Deduplicate expected findings by fingerprint (a case shouldn't expect the
	// same thing twice, but be deterministic about it).
	available := make([]ExpectedFinding, 0, len(expected))
	seenExp := map[string]bool{}
	for _, e := range expected {
		k := e.Fingerprint()
		if k == "" || seenExp[k] {
			continue
		}
		seenExp[k] = true
		available = append(available, e)
	}
	sort.Slice(available, func(i, j int) bool { return available[i].Fingerprint() < available[j].Fingerprint() })

	matched := map[int]bool{} // index into available
	out := MatchResult{Matched: []FindingMatch{}}
	for _, a := range deduped {
		for i := range available {
			if matched[i] {
				continue
			}
			if available[i].matches(a) {
				out.Matched = append(out.Matched, FindingMatch{Expected: available[i], Actual: a})
				matched[i] = true
				break
			}
		}
	}
	for i := range available {
		if !matched[i] {
			out.UnmatchedExpected = append(out.UnmatchedExpected, available[i])
		}
	}
	for _, a := range deduped {
		found := false
		for _, mm := range out.Matched {
			if mm.Actual.Fingerprint == a.Fingerprint {
				found = true
				break
			}
		}
		if !found {
			out.UnmatchedActual = append(out.UnmatchedActual, a)
		}
	}
	SortStable(out)
	return out
}

// SortStable sorts the unmatched lists deterministically by fingerprint.
func SortStable(m MatchResult) MatchResult {
	sort.Slice(m.Matched, func(i, j int) bool { return m.Matched[i].Actual.Fingerprint < m.Matched[j].Actual.Fingerprint })
	sort.Slice(m.UnmatchedExpected, func(i, j int) bool { return m.UnmatchedExpected[i].Fingerprint() < m.UnmatchedExpected[j].Fingerprint() })
	sort.Slice(m.UnmatchedActual, func(i, j int) bool { return m.UnmatchedActual[i].Fingerprint < m.UnmatchedActual[j].Fingerprint })
	return m
}

// ConfusionMatrix is the confusion matrix over one case or a suite (§D1).
type ConfusionMatrix struct {
	TP int `json:"tp"`
	FP int `json:"fp"`
	TN int `json:"tn"`
	FN int `json:"fn"`
}

func (m ConfusionMatrix) Total() int { return m.TP + m.FP + m.TN + m.FN }

// FromMatchResult fills the confusion matrix from a MatchResult. TN =
// expected-negative findings that were not produced.
func (m *ConfusionMatrix) FromMatchResult(r MatchResult) {
	m.TP = len(r.Matched)
	m.FP = len(r.UnmatchedActual)
	m.FN = len(r.UnmatchedExpected)
}

// NodeToActualFinding converts a graph finding node + findings-table row into the
// benchmark's observed form.
func NodeToActualFinding(node *db.Node, findingID int64, evidenceState string, evidenceHasBody bool) ActualFinding {
	var payload struct {
		VulnClass string `json:"vulnclass"`
		Name      string `json:"name"`
		Severity  string `json:"severity"`
	}
	_ = json.Unmarshal(node.Payload, &payload)
	a := ActualFinding{
		NodeID:          node.ID,
		FindingID:       findingID,
		VulnClass:       payload.VulnClass,
		Name:            payload.Name,
		Severity:        payload.Severity,
		Worker:          node.Origin,
		EvidenceState:   evidenceState,
		EvidenceHasBody: evidenceHasBody,
	}
	a.Fingerprint = fingerprint(a.VulnClass, a.Name, a.Severity)
	return a
}

func fingerprint(vulnClass, name, severity string) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{vulnClass, name, severity} {
		if p = normalizeText(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\x00")
}
