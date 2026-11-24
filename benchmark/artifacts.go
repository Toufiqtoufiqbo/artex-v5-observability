package benchmark

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)
// artifacts.go — versioned, machine-readable benchmark artifacts (§H2):
// run manifests + case results + suite metrics in one JSON file, loaded and
// validated by `artex benchmark compare` (§8, §H3).
//
// Secrets: artifacts never contain API keys, cookies, tokens or DSNs. The
// config fingerprint omits/normalizes secret fields.

// RunManifest describes one run (§H2). Fields under Version/GitHead/TreeFingerprint
// identify the exact code the run measured.
type RunManifest struct {
	RunID               string    `json:"run_id"`
	Version             string    `json:"version"` // ARTEX build version (server.BuildVersion)
	GitHead             string    `json:"git_head,omitempty"`
	TreeFingerprint     string    `json:"worktree_fingerprint"`
	BenchmarkSuite      string    `json:"benchmark_suite"`
	SuiteVersion        string    `json:"suite_version"`
	Cases               int       `json:"cases"`
	Mode                string    `json:"mode"` // offline | live
	Seed                int64     `json:"seed"`
	Os                  string    `json:"os"`
	Arch                string    `json:"arch"`
	GoVersion           string    `json:"go_version"`
	ExecutionMode       string    `json:"execution_mode"`
	StartedAt           time.Time `json:"started_at"`
	FinishedAt          time.Time `json:"finished_at"`
	DurationSecs        float64   `json:"duration_secs"`
	ConfigFingerprint   string    `json:"config_fingerprint"`
	Redactions          int       `json:"secret_redactions"`
	WorktreeEntries     int       `json:"worktree_entries"`
}

// RunArtifact is the top-level artifact file.
type RunArtifact struct {
	Manifest RunManifest `json:"manifest"`
	Metrics  SuiteMetrics `json:"metrics"`
	Cases    []CaseMetrics `json:"cases"`
}

func NewRunID(mode string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("bench-%s-%s-%s", mode, time.Now().UTC().Format("20060102T150405Z"), hex.EncodeToString(b[:]))
}

// SecretEnvKeys lists environment keys whose values must never enter artifacts.
var SecretEnvKeys = map[string]bool{
	"ARTEX_PG_DSN": true, "ARTEX_API_KEY": true, "ANTHROPIC_API_KEY": true,
	"OPENAI_API_KEY": true, "BRAVE_API_KEY": true, "TAVILY_API_KEY": true,
	"DEEPSEEK_API_KEY": true, "BENCH_TOKEN": true, "GITHUB_TOKEN": true,
}

// SafeConfigFingerprint returns a sha256 fingerprint of the safe subset of the
// environment (§H2: secrets omitted or securely normalized) + redaction count.
func SafeConfigFingerprint(env map[string]string) (string, int) {
	redactions := 0
	keys := make([]string, 0, len(env))
	for k := range env {
		if SecretEnvKeys[k] {
			redactions++ // counted, never hashed in
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(env[k])
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), redactions
}

func gitOutput(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Validate checks the manifest for sanity (§H2).
func (m RunManifest) Validate() []string {
	var errs []string
	if m.RunID == "" {
		errs = append(errs, "manifest.run_id empty")
	}
	if m.BenchmarkSuite == "" {
		errs = append(errs, "manifest.benchmark_suite empty")
	}
	if m.SuiteVersion == "" {
		errs = append(errs, "manifest.suite_version empty")
	}
	if m.Mode != "offline" && m.Mode != "live" {
		errs = append(errs, fmt.Sprintf("manifest.mode=%q not in {offline,live}", m.Mode))
	}
	if m.StartedAt.IsZero() || m.FinishedAt.IsZero() {
		errs = append(errs, "manifest start/finish timestamps missing")
	}
	return errs
}

// EnvironmentSnapshot returns the safe, redacted environment + fingerprint.
func EnvironmentSnapshot() (map[string]string, string, int) {
	env := map[string]string{}
	for _, k := range os.Environ() {
		kv := strings.SplitN(k, "=", 2)
		if len(kv) == 2 {
			env[kv[0]] = kv[1]
		}
	}
	fp, redactions := SafeConfigFingerprint(env)
	return env, fp, redactions
}

func (ra *RunArtifact) Save(path string) error {
	b, err := json.MarshalIndent(ra, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

func LoadRunArtifact(path string) (*RunArtifact, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ra RunArtifact
	if err := json.Unmarshal(b, &ra); err != nil {
		return nil, fmt.Errorf("parse run artifact %s: %w", path, err)
	}
	return &ra, nil
}

// Validate runs the §H3 artifact validation: manifest sanity + per-case
// invariants (confusion matrix consistency, DTO-level consistency).
func (ra *RunArtifact) Validate() []string {
	var errs []string
	m := ra.Manifest
	if m.RunID == "" {
		errs = append(errs, "manifest.run_id empty")
	}
	if m.BenchmarkSuite == "" {
		errs = append(errs, "manifest.benchmark_suite empty")
	}
	if m.Mode != "offline" && m.Mode != "live" {
		errs = append(errs, fmt.Sprintf("manifest.mode=%q not in {offline,live}", m.Mode))
	}
	if m.StartedAt.IsZero() || m.FinishedAt.IsZero() {
		errs = append(errs, "manifest start/finish timestamps missing")
	}
	validStatus := map[string]bool{"completed": true, "timed_out": true, "cancelled": true, "execution_error": true, "incomplete": true}
	for _, c := range ra.Cases {
		if !validStatus[c.Status] {
			errs = append(errs, fmt.Sprintf("case %s: invalid status %q", c.CaseID, c.Status))
		}
		if c.Metrics.TP != c.CM.TP || c.Metrics.FP != c.CM.FP || c.Metrics.TN != c.CM.TN || c.Metrics.FN != c.CM.FN {
			errs = append(errs, fmt.Sprintf("case %s: metrics vs cm mismatch", c.CaseID))
		}
		if c.Metrics.ActualFindings != len(c.ActualFindings) {
			errs = append(errs, fmt.Sprintf("case %s: metrics.actual_findings != len(actual_findings)", c.CaseID))
		}
		if len(c.MatchResult.Matched) != c.Metrics.TP {
			errs = append(errs, fmt.Sprintf("case %s: match_result.matched != metrics.tp", c.CaseID))
		}
	}
	return errs
}

// --- comparison (§G2: version vs candidate comparison support) ---

type CaseComparison struct {
	CaseID          string            `json:"case_id"`
	StatusA         string            `json:"status_a"`
	StatusB         string            `json:"status_b"`
	LostTP          int               `json:"lost_tp"`
	GainedTP        int               `json:"gained_tp"`
	LostExpected    []ExpectedFinding `json:"lost_expected,omitempty"`
	GainedExpected  []ExpectedFinding `json:"gained_expected,omitempty"`
	LostFPsA        int               `json:"lost_fp_a"`
	GainedFPsB      int               `json:"gained_fp_b"`
	LostFindings    []ActualFinding   `json:"lost_findings,omitempty"`
	GainedFindings  []ActualFinding   `json:"gained_findings,omitempty"`
	A               CaseMetrics       `json:"a"`
	B               CaseMetrics       `json:"b"`
}

type RunComparison struct {
	RunA          string            `json:"run_a"`
	RunB          string            `json:"run_b"`
	SuiteA        string            `json:"suite_a"`
	SuiteB        string            `json:"suite_b"`
	ModeA         string            `json:"mode_a"`
	ModeB         string            `json:"mode_b"`
	VersionA      string            `json:"version_a"`
	VersionB      string            `json:"version_b"`
	GitHeadA      string            `json:"git_head_a,omitempty"`
	GitHeadB      string            `json:"git_head_b,omitempty"`
	Incompatible  string            `json:"incompatible,omitempty"` // incompatible_runs | ok
	Cases         []CaseComparison  `json:"cases"`
	MetricsA      Metrics           `json:"metrics_a"`
	MetricsB      Metrics           `json:"metrics_b"`
}

func fingerprintsOf(acts []ActualFinding) map[string]ActualFinding {
	out := map[string]ActualFinding{}
	for _, a := range acts {
		if a.Fingerprint != "" {
			out[a.Fingerprint] = a
		}
	}
	return out
}

// ComputeComparison compares two artifacts case-by-case by stable case ID
// (§G2). It detects incompatible runs: differing suite or version (or case sets).
func ComputeComparison(a, b *RunArtifact) *RunComparison {
	out := &RunComparison{RunA: a.Manifest.RunID, RunB: b.Manifest.RunID,
		SuiteA: a.Manifest.BenchmarkSuite, SuiteB: b.Manifest.BenchmarkSuite,
		ModeA: a.Manifest.Mode, ModeB: b.Manifest.Mode,
		VersionA: a.Manifest.Version, VersionB: b.Manifest.Version,
		GitHeadA: a.Manifest.GitHead, GitHeadB: b.Manifest.GitHead,
		MetricsA: a.Metrics.Micro, MetricsB: b.Metrics.Micro}

	casesA := map[string]CaseMetrics{}
	for _, c := range a.Cases {
		casesA[c.CaseID] = c
	}
	casesB := map[string]CaseMetrics{}
	for _, c := range b.Cases {
		casesB[c.CaseID] = c
	}
	for id := range casesA {
		if _, ok := casesB[id]; !ok {
			out.Incompatible = "case sets differ"
			break
		}
	}
	for id := range casesB {
		if _, ok := casesA[id]; !ok {
			out.Incompatible = "case sets differ"
			break
		}
	}
	if out.SuiteA != out.SuiteB || out.VersionA != out.VersionB {
		out.Incompatible = "incompatible_runs: suites/versions differ"
	}
	for id := range casesA {
		ca := casesA[id]
		cb, ok := casesB[id]
		if !ok {
			continue
		}
		cc := CaseComparison{CaseID: id, StatusA: ca.Status, StatusB: cb.Status, A: ca, B: cb}
		fpA := fingerprintsOf(ca.ActualFindings)
		fpB := fingerprintsOf(cb.ActualFindings)
		for _, mm := range ca.MatchResult.Matched {
			if _, ok := fpB[mm.Actual.Fingerprint]; !ok {
				cc.LostTP++
				cc.LostExpected = append(cc.LostExpected, mm.Expected)
			}
		}
		for _, mm := range cb.MatchResult.Matched {
			if _, ok := fpA[mm.Actual.Fingerprint]; !ok {
				cc.GainedTP++
				cc.GainedExpected = append(cc.GainedExpected, mm.Expected)
			}
		}
		for _, af := range ca.ActualFindings {
			if _, ok := fpB[af.Fingerprint]; !ok {
				cc.LostFPsA++
				cc.LostFindings = append(cc.LostFindings, af)
			}
		}
		for _, af := range cb.ActualFindings {
			if _, ok := fpA[af.Fingerprint]; !ok {
				cc.GainedFPsB++
				cc.GainedFindings = append(cc.GainedFindings, af)
			}
		}
		out.Cases = append(out.Cases, cc)
	}
	if out.Incompatible == "" {
		out.Incompatible = "ok"
	}
	return out
}
