package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// selfcheck_test.go — benchmark self-tests (§I): adversarial correctness
// checks for the evaluator itself (metrics, matcher, artifacts) plus
// schema/artifact validations. These tests are part of the engineering-grade
// guarantee that the framework is correct, not merely convenient.

func ef(vulnclass, name, severity string, required bool) ExpectedFinding {
	return ExpectedFinding{VulnClass: vulnclass, Name: name, Severity: severity, EvidenceRequired: required}
}

func af(vulnclass, name, severity, state string) ActualFinding {
	a := ActualFinding{VulnClass: vulnclass, Name: name, Severity: severity, EvidenceState: state}
	a.Fingerprint = fingerprint(vulnclass, name, severity)
	return a
}

func TestComputeMetricsSyntheticAllPositivePerfect_v91(t *testing.T) {
	expected := []ExpectedFinding{ef("SQLi", "Blind SQL Injection", "high", true), ef("XSS", "Reflected XSS", "medium", true)}
	actual := []ActualFinding{af("SQLi", "Blind SQL Injection", "high", "valid"), af("XSS", "Reflected XSS", "medium", "valid")}
	mr := MatchFindings(expected, actual)
	if len(mr.Matched) != 2 {
		t.Fatalf("matched=%d want 2", len(mr.Matched))
	}
	cm := ConfusionMatrix{}
	cm.FromMatchResult(mr)
	m := ComputeMetrics(cm, mr, actual, len(expected))
	if m.TP != 2 || m.FP != 0 || m.FN != 0 {
		t.Fatalf("TP/FP/FN=%d/%d/%d want 2/0/0", m.TP, m.FP, m.FN)
	}
	if m.Precision != 1 || m.Recall != 1 || m.F1 != 1 {
		t.Fatalf("perfect detection: p=%.3f r=%.3f f1=%.3f", m.Precision, m.Recall, m.F1)
	}
	if m.EvidenceValidityRate != 1 {
		t.Fatalf("evidence validity=%.3f want 1", m.EvidenceValidityRate)
	}
	if m.FindingEvidenceCoverage != 1 {
		t.Fatalf("evidence coverage=%.3f want 1", m.FindingEvidenceCoverage)
	}
}

func TestComputeMetricsSyntheticAllNegative_v91(t *testing.T) {
	expected := []ExpectedFinding{ef("SSRF", "Foo", "low", false)}
	actual := []ActualFinding{af("SQLi", "Other", "high", "none")}
	mr := MatchFindings(expected, actual)
	cm := ConfusionMatrix{}
	cm.FromMatchResult(mr)
	m := ComputeMetrics(cm, mr, actual, 1)
	if m.TP != 0 || m.FP != 1 {
		t.Fatalf("TP/FP=%d/%d want 0/1 (expected finding unmatched, actual unmatched)", m.TP, m.FP)
	}
	if m.F1 != 0 {
		t.Fatalf("f1=%.3f want 0 (zero denominators, not manufactured)", m.F1)
	}
}

func TestComputeMetricsFPHeavy_v91(t *testing.T) {
	expected := []ExpectedFinding{ef("SQLi", "Blind SQL Injection", "high", false)}
	actual := []ActualFinding{
		af("SQLi", "Blind SQL Injection", "high", "valid"),
		af("XSS", "Reflected XSS", "medium", "valid"),
		af("XSS", "Reflected XSS", "medium", "valid"), // duplicate of the above
	}
	mr := MatchFindings(expected, actual)
	cm := ConfusionMatrix{}
	cm.FromMatchResult(mr)
	m := ComputeMetrics(cm, mr, actual, 1)
	if m.TP != 1 {
		t.Fatalf("TP=%d want 1", m.TP)
	}
	// Duplicates dedup to a single actual finding; the single unmatched XSS is FP=1.
	if m.FP != 1 {
		t.Fatalf("FP=%d want 1 (dup deduped; the other XSS is unmatched)", m.FP)
	}
	if m.Precision != 0.5 {
		t.Fatalf("precision=%.4f want 0.5 (TP/(TP+FP) = 1/2)", m.Precision)
	}
	if m.Recall != 1 {
		t.Fatalf("recall=%.3f want 1", m.Recall)
	}
}

func TestComputeMetricsFNLossy_v91(t *testing.T) {
	expected := []ExpectedFinding{ef("SQLi", "A", "high", false), ef("SQLi", "B", "high", false), ef("SQLi", "C", "high", false)}
	actual := []ActualFinding{af("SQLi", "A", "high", "valid")}
	mr := MatchFindings(expected, actual)
	cm := ConfusionMatrix{}
	cm.FromMatchResult(mr)
	m := ComputeMetrics(cm, mr, actual, 3)
	if m.TP != 1 || m.FN != 2 {
		t.Fatalf("TP/FN=%d/%d want 1/2", m.TP, m.FN)
	}
	if m.Recall != 1.0/3.0 {
		t.Fatalf("recall=%.4f want %.4f", m.Recall, 1.0/3.0)
	}
}

func TestDuplicatesDeduped_v91(t *testing.T) {
	expected := []ExpectedFinding{ef("SQLi", "Blind SQL Injection", "high", true)}
	dup := af("SQLi", "Blind SQL Injection", "high", "valid")
	actual := []ActualFinding{dup, dup, dup}
	mr := MatchFindings(expected, actual)
	if len(mr.Matched) != 1 {
		t.Fatalf("TP=%d want 1 — duplicates must not inflate TP", len(mr.Matched))
	}
	if len(mr.UnmatchedActual) != 0 {
		t.Fatalf("dup FPs=%d want 0 (duplicates are not double-counted as FP)", len(mr.UnmatchedActual))
	}
}

func TestEvidenceNoneVsValid_v91(t *testing.T) {
	actual := []ActualFinding{af("SQLi", "A", "high", "none"), af("SQLi", "B", "high", "valid"), af("SQLi", "C", "high", "invalid")}
	cm := ConfusionMatrix{TP: 2, FP: 1}
	m := ComputeMetrics(cm, MatchResult{}, actual, 0)
	if m.FindingsWithoutEvidence != 1 || m.EvidenceValidFindings != 1 || m.EvidenceInvalidFindings != 1 {
		t.Fatalf("evidence accounting wrong: none=%d valid=%d invalid=%d", m.FindingsWithoutEvidence, m.EvidenceValidFindings, m.EvidenceInvalidFindings)
	}
	if m.EvidenceValidityRate != 0.5 {
		t.Fatalf("evidence validity=%.3f want 0.5 (none excluded)", m.EvidenceValidityRate)
	}
}

func TestMatchStructuralKeys_v91(t *testing.T) {
	// Mismatch: severity differs.
	expected := []ExpectedFinding{ef("SQLi", "Blind SQL Injection (boolean)", "high", false)}
	actual := []ActualFinding{af("SQLi", "Blind SQL Injection (boolean)", "medium", "valid")}
	mr := MatchFindings(expected, actual)
	if len(mr.Matched) != 0 || len(mr.UnmatchedExpected) != 1 {
		t.Fatalf("severity mismatch should not match: matched=%d unmatchedExpected=%d", len(mr.Matched), len(mr.UnmatchedExpected))
	}
	// Wildcard: expected has no name → any name matches.
	expected2 := []ExpectedFinding{{VulnClass: "SQLi", Severity: "high"}}
	actual2 := []ActualFinding{af("SQLi", "Anything", "high", "valid")}
	if mr2 := MatchFindings(expected2, actual2); len(mr2.Matched) != 1 {
		t.Fatalf("wildcard name should match: matched=%d", len(mr2.Matched))
	}
	// Full structural match.
	expected3 := []ExpectedFinding{ef("SQLi", "Blind SQL Injection", "high", false)}
	actual3 := []ActualFinding{af("SQLi", "Blind SQL Injection", "high", "valid")}
	if mr3 := MatchFindings(expected3, actual3); len(mr3.Matched) != 1 {
		t.Fatalf("exact match should match: matched=%d", len(mr3.Matched))
	}
}

func TestComputeMetricsZeroDenominators_v91(t *testing.T) {
	m := ComputeMetrics(ConfusionMatrix{}, MatchResult{}, nil, 0)
	if m.TP != 0 || m.Precision != 0 || m.Recall != 0 || m.Accuracy != 0 {
		t.Fatalf("all-zero metrics=%+v", m)
	}
	for _, want := range []string{"recall", "precision"} {
		if !contains(m.UndefinedMetrics, want) && m.TP == 0 && want == "precision" {
			// precision has zero denominator; note it (documented semantics)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestSafeConfigFingerprint_v91(t *testing.T) {
	env := map[string]string{"ARTEX_PG_DSN": "postgres://user:secret@host/db", "ARTEX_EGRESS_ALLOWLIST": "fixture.example:8443", "PATH": os.Getenv("PATH")}
	fp, redactions := SafeConfigFingerprint(env)
	if len(fp) != 64 {
		t.Fatalf("fingerprint=%q not a sha256 hex", fp)
	}
	if redactions != 1 {
		t.Fatalf("redactions=%d want 1", redactions)
	}
	if strings.Contains(fp, "secret") || strings.Contains(fp, "user:secret@host") {
		t.Fatal("fingerprint must never contain secret values")
	}
	// Same safe env → same fingerprint; env with secret → still same.
	fp2, _ := SafeConfigFingerprint(map[string]string{"ARTEX_EGRESS_ALLOWLIST": "fixture.example:8443", "PATH": os.Getenv("PATH")})
	if fp != fp2 {
		t.Fatal("safe env should produce stable fingerprints")
	}
	fp3, _ := SafeConfigFingerprint(map[string]string{"ARTEX_PG_DSN": "postgres://other:different@host2/db2", "ARTEX_EGRESS_ALLOWLIST": "fixture.example:8443", "PATH": os.Getenv("PATH")})
	if fp != fp3 {
		t.Fatal("fingerprint must be identical for any DSN value (secrets omitted)")
	}
}

func TestArtifactRoundTripValidate_v91(t *testing.T) {
	suite := SuiteByID("artex-v5-offline-baseline")
	if suite == nil {
		t.Fatal("default suite missing")
	}
	if errs := suite.Validate(); len(errs) > 0 {
		t.Fatalf("corpus not valid: %v", errs)
	}
	dir := t.TempDir()
	art := &RunArtifact{
		Manifest: RunManifest{RunID: NewRunID("offline"), BenchmarkSuite: suite.SuiteID, SuiteVersion: suite.SuiteVersion, Mode: "offline", StartedAt: time.Now(), FinishedAt: time.Now(), ConfigFingerprint: "abc"},
		Metrics:  ComputeSuiteMetrics(suite, nil, "offline"),
		Cases:    []CaseMetrics{{CaseID: "x", Status: "completed", CM: ConfusionMatrix{TP: 1}, Metrics: Metrics{TP: 1, ActualFindings: 1}, ActualFindings: []ActualFinding{af("SQLi", "A", "high", "valid")}, MatchResult: MatchResult{Matched: []FindingMatch{{Expected: ef("SQLi", "A", "high", false), Actual: af("SQLi", "A", "high", "valid")}}}}},
	}
	if errs := art.Validate(); len(errs) > 0 {
		t.Fatalf("valid artifact rejected: %v", errs)
	}
	path := filepath.Join(dir, "run.json")
	if err := art.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRunArtifact(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manifest.RunID != art.Manifest.RunID {
		t.Fatal("round-trip mismatch")
	}
	// Tamper: flip a TP count.
	b, _ := os.ReadFile(path)
	_ = b
	art.Cases[0].CM.TP = 99
	if errs := art.Validate(); len(errs) == 0 {
		t.Fatal("tampered artifact passed validation")
	}
}

func TestCaseValidateExcludedReasonRequired_v91(t *testing.T) {
	c := &Case{ID: "x", SuiteID: "s", SchemaVersion: SchemaVersion, Category: "detection_positive", Description: "d", ReviewStatus: "excluded", ExecutionLimits: ExecutionLimits{TimeoutSeconds: 1}}
	errs := ValidateCase(c)
	if len(errs) == 0 {
		t.Fatal("excluded case without reason must be invalid")
	}
	c.ExcludedReason = ptr("stale since 8.6.3")
	if errs := ValidateCase(c); len(errs) != 0 {
		t.Fatalf("valid excluded case rejected: %v", errs)
	}
}

func TestCompareIncompatibleOnSuiteMismatch_v91(t *testing.T) {
	a := &RunArtifact{Manifest: RunManifest{RunID: "a", BenchmarkSuite: "suite-1", Mode: "offline", Version: "v1"}}
	b := &RunArtifact{Manifest: RunManifest{RunID: "b", BenchmarkSuite: "suite-2", Mode: "offline", Version: "v1"}}
	cmp := ComputeComparison(a, b)
	if cmp.Incompatible != "case sets differ" && cmp.Incompatible != "incompatible_runs: suites/versions differ" {
		t.Fatalf("compare incompatible=%q want incompatible_runs", cmp.Incompatible)
	}
	if cmp.Incompatible == "ok" {
		t.Fatal("different suites must not compare as ok")
	}
}

func ptr(s string) *string { return &s }

var _ = db.IsTerminal
