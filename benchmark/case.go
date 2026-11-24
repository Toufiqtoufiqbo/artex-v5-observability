// Package benchmark implements Phase IX.1: an engineering-grade benchmark and
// evaluation framework for ARTEX v5's Planner/Worker execution pipeline.
//
// Design (Phase IX.1 — MEASURE BEFORE OPTIMIZATION):
//
//   - Cases are versioned, reviewed ground truth. Ground truth (expected findings,
//     negative findings, evidence requirements) is authored by the benchmark,
//     never derived from ARTEX output (§11: no circularity).
//   - Cases run through the REAL engine (server.Engine + agent.Planner +
//     agent.Worker) against isolated disposable PostgreSQL databases (§16).
//   - The default "deterministic offline" mode uses a scripted llm.Provider
//     (mock agent) so the benchmark can measure TP/FP/FN/TN deterministically;
//     it must not be presented as proof of real LLM performance (§5).
//   - A "live model" mode is supported and uses only an already-configured,
//     already-authorized provider chain from llm_profiles; no new provider
//     integration is added.
//   - Metrics, timing and reliability are computed from existing ARTEX
//     telemetry (db.Activity, llm_usage, task lifecycle).
package benchmark

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Case is one benchmark case. The schema is the ground-truth contract; it is
// validated by Suite.Validate (see ValidateCase).
type Case struct {
	ID            string `json:"case_id"`
	SuiteID       string `json:"suite_id"`
	SchemaVersion string `json:"schema_version"`
	Category      string `json:"category"` // detection_positive | detection_negative | lifecycle_edge
	Description   string `json:"description"`
	FixtureVersion string `json:"fixture_version"`

	EnvironmentRequirements map[string]string `json:"environment_requirements,omitempty"`

	ExpectedObservations []Observation             `json:"expected_observations,omitempty"`
	ExpectedFindings     []ExpectedFinding         `json:"expected_findings"`
	ExpectedNegativeFindings []ExpectedFinding      `json:"expected_negative_findings,omitempty"`
	ExpectedEvidenceRequirements []EvidenceRequirement `json:"expected_evidence_requirements,omitempty"`

	ExecutionLimits ExecutionLimits `json:"execution_limits"`
	Tags            []string        `json:"tags"`
	GroundTruthVersion string       `json:"ground_truth_version"`
	ReviewStatus    string          `json:"review_status"` // reviewed | pending | excluded
	ExcludedReason  *string         `json:"excluded_reason,omitempty"`

	// Data carries case-specific static inputs (none required by the v1 schema;
	// reserved for future fixtures).
	Data map[string]any `json:"data,omitempty"`

	// Script is the deterministic execution script authored by the benchmark
	// (offline mode only). It is ground truth for what the mock LLM will ask
	// the engine to do; ARTEX's actual output is compared against the
	// independent expected_findings above.
	Script []ScriptResponse `json:"script,omitempty"`
}

// Observation is an expected non-finding observation of the graph
// (facts/assets/notes), for lifecycle-edge cases that must verify the pipeline
// wrote something even when no finding is expected.
type Observation struct {
	Kind    string `json:"kind"` // fact | asset | note
	Summary string `json:"summary"`
	AssetURL string `json:"asset_url,omitempty"`
}

// ExpectedFinding is the ground-truth fingerprint of one finding the case
// expects ARTEX to produce. Matching uses the structurally-identifiable fields
// (vuln_class, name, severity); see matcher.go.
type ExpectedFinding struct {
	VulnClass string `json:"vuln_class"`
	Name      string `json:"name,omitempty"`
	Severity  string `json:"severity,omitempty"`
	// MatchKey, when set, overrides the structural fingerprint (for cases whose
	// canonical names are environment-specific, e.g. cURL path templates).
	MatchKey  string `json:"match_key,omitempty"`
	// EvidenceRequired marks TPs as requiring valid supporting evidence.
	EvidenceRequired bool `json:"evidence_required"`
}

// EvidenceRequirement records per-finding evidence expectations for exports
// and self-checks (advisory until the artifact/graph evidence verifiers run).
type EvidenceRequirement struct {
	MatchKey        string   `json:"match_key"`
	MinEvidenceTypes []string `json:"min_evidence_types,omitempty"`
}

// ExecutionLimits bounds one case: hard wall clock and the max number of
// seeded open intents. Zero/negative = engine defaults.
type ExecutionLimits struct {
	TimeoutSeconds int `json:"timeout_seconds"`
	MaxIntents     int `json:"max_intents"`
}

func ValidCategory() map[string]bool { return map[string]bool{"detection_positive": true, "detection_negative": true, "lifecycle_edge": true} }

func ValidReviewStatus() map[string]bool { return map[string]bool{"reviewed": true, "pending": true, "excluded": true} }

// ValidateCase checks the ground-truth contract of one case.
func ValidateCase(c *Case) []string {
	var errs []string
	if c == nil {
		return []string{"case: nil"}
	}
	if strings.TrimSpace(c.ID) == "" {
		errs = append(errs, "case: case_id empty")
	}
	if c.SchemaVersion != SchemaVersion {
		errs = append(errs, fmt.Sprintf("case %s: schema_version=%q want %q", c.ID, c.SchemaVersion, SchemaVersion))
	}
	if strings.TrimSpace(c.SuiteID) == "" {
		errs = append(errs, "case: suite_id empty")
	}
	if !ValidCategory()[c.Category] {
		errs = append(errs, fmt.Sprintf("case %s: invalid category %q", c.ID, c.Category))
	}
	if strings.TrimSpace(c.Description) == "" {
		errs = append(errs, fmt.Sprintf("case %s: description empty", c.ID))
	}
	if !ValidReviewStatus()[c.ReviewStatus] {
		errs = append(errs, fmt.Sprintf("case %s: invalid review_status %q", c.ID, c.ReviewStatus))
	}
	if c.ReviewStatus == "excluded" && (c.ExcludedReason == nil || strings.TrimSpace(*c.ExcludedReason) == "") {
		errs = append(errs, fmt.Sprintf("case %s: excluded reason required", c.ID))
	}
	if c.ExecutionLimits.TimeoutSeconds <= 0 {
		errs = append(errs, fmt.Sprintf("case %s: execution_limits.timeout_seconds must be > 0", c.ID))
	}
	for _, ef := range c.ExpectedFindings {
		if strings.TrimSpace(ef.VulnClass) == "" {
			errs = append(errs, fmt.Sprintf("case %s: expected finding without vuln_class", c.ID))
		}
	}
	// Evidence requirements must reference existing findings (expected or negative).
	allowed := map[string]bool{}
	for _, ef := range c.ExpectedFindings { allowed[ef.Fingerprint()] = true }
	for _, ef := range c.ExpectedNegativeFindings { allowed[ef.Fingerprint()] = true }
	for _, er := range c.ExpectedEvidenceRequirements {
		if !allowed[er.MatchKey] {
			errs = append(errs, fmt.Sprintf("case %s: evidence requirement %q does not match an expected/negative finding", c.ID, er.MatchKey))
		}
	}
	for _, obs := range c.ExpectedObservations {
		if strings.TrimSpace(obs.Summary) == "" {
			errs = append(errs, fmt.Sprintf("case %s: observation without summary", c.ID))
		}
	}
	return errs
}

// FindingsByVulnClass indexes the case's expected findings by vuln_class.
func (c *Case) FindingsByVulnClass() map[string][]ExpectedFinding {
	out := map[string][]ExpectedFinding{}
	for _, ef := range c.ExpectedFindings {
		k := strings.ToLower(strings.TrimSpace(ef.VulnClass))
		out[k] = append(out[k], ef)
	}
	return out
}

func (c *Case) JSON() string {
	b, _ := json.MarshalIndent(c, "", "  ")
	return string(b)
}

// Suite is a versioned, reviewable benchmark suite.
type Suite struct {
	SuiteID      string    `json:"suite_id"`
	SuiteVersion string    `json:"suite_version"`
	Description  string    `json:"description"`
	CreatedAt    time.Time `json:"created_at"`
	Cases        []*Case   `json:"cases"`
}

// SchemaVersion is the current case/suite schema version.
const SchemaVersion = "1"

func (s *Suite) Validate() []string {
	var errs []string
	if strings.TrimSpace(s.SuiteID) == "" {
		errs = append(errs, "suite: suite_id empty")
	}
	if s.SuiteVersion != SchemaVersion {
		errs = append(errs, fmt.Sprintf("suite %s: unsupported suite_version %q", s.SuiteID, s.SuiteVersion))
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		if seen[c.ID] {
			errs = append(errs, fmt.Sprintf("suite %s: duplicate case id %q", s.SuiteID, c.ID))
		}
		seen[c.ID] = true
		errs = append(errs, ValidateCase(c)...)
		if c.SuiteID != s.SuiteID {
			errs = append(errs, fmt.Sprintf("suite %s: case %s has different suite_id %q", s.SuiteID, c.ID, c.SuiteID))
		}
	}
	return errs
}
