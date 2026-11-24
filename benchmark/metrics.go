package benchmark

import (
	"sort"
	"time"
)

// metrics.go — correct, independently tested evaluation metrics (§D2), timing
// statistics (§E1), model-usage (§E2) and reliability (§E4) views.
//
// Zero-denominator policy: undefined ratios are reported with UndefinedMetrics,
// never as zero. Missing telemetry stays "unknown" rather than zero (§C4).

// Metrics is the §D2 confusion matrix + detection metrics of one case or a suite.
type Metrics struct {
	TP int `json:"tp"`
	FP int `json:"fp"`
	TN int `json:"tn"`
	FN int `json:"fn"`

	// Derived ratios; zero if denominator zero (see UndefinedMetrics).
	Precision         float64  `json:"precision"`
	Recall            float64  `json:"recall"`
	F1                float64  `json:"f1"`
	Specificity       float64  `json:"specificity"`
	FalsePositiveRate float64  `json:"false_positive_rate"`
	FalseNegativeRate float64  `json:"false_negative_rate"`
	Accuracy          float64  `json:"accuracy"`
	UndefinedMetrics  []string `json:"undefined_metrics,omitempty"`

	// Case completion rate (§C4): cases completed (terminal task OR all seeded
	// intents terminal).
	CaseCompletionRate float64 `json:"case_completion_rate"`

	// Evidence metrics (§D2: evidence validity rate + finding evidence coverage).
	EvidenceValidityRate    float64 `json:"evidence_validity_rate"`
	FindingsWithoutEvidence int     `json:"findings_without_evidence"`
	EvidenceValidFindings   int     `json:"evidence_valid_findings"`
	EvidenceInvalidFindings int     `json:"evidence_invalid_findings"`
	EvidenceUnknownFindings int     `json:"evidence_unknown_findings"`
	FindingsTotal           int     `json:"findings_total"`
	FindingEvidenceCoverage float64 `json:"finding_evidence_coverage"`

	ExpectedFindings int `json:"expected_findings"`
	ActualFindings   int `json:"actual_findings"`
}

func (m Metrics) Completed() bool { return m.CaseCompletionRate > 0 }

// ComputeMetrics computes all §D2 detection metrics from the confusion matrix and
// the match result.
func ComputeMetrics(cm ConfusionMatrix, mr MatchResult, actualFindings []ActualFinding, expectedCount int) Metrics {
	m := Metrics{TP: cm.TP, FP: cm.FP, TN: cm.TN, FN: cm.FN}
	m.Precision = divOr0(cm.TP, cm.TP+cm.FP)
	m.Recall = divOr0(cm.TP, cm.TP+cm.FN)
	m.Specificity = divOr0(cm.TN, cm.TN+cm.FP)
	m.FalsePositiveRate = divOr0(cm.FP, cm.TN+cm.FP)
	m.FalseNegativeRate = divOr0(cm.FN, cm.TP+cm.FN)
	m.Accuracy = divOr0(cm.TP+cm.TN, cm.Total())
	m.F1 = divOr0f(2*m.Precision*m.Recall, m.Precision+m.Recall)
	if cm.TP > 0 && m.Precision+m.Recall == 0 {
		m.UndefinedMetrics = append(m.UndefinedMetrics, "f1")
	}
	m.ExpectedFindings = expectedCount
	m.ActualFindings = len(actualFindings)

	// Evidence metrics over observed findings.
	for _, a := range actualFindings {
		switch a.EvidenceState {
		case "valid":
			m.EvidenceValidFindings++
		case "invalid":
			m.EvidenceInvalidFindings++
		case "unknown":
			m.EvidenceUnknownFindings++
		case "none", "":
			m.FindingsWithoutEvidence++
		}
	}
	m.FindingsTotal = len(actualFindings)
	if den := m.FindingsTotal - m.FindingsWithoutEvidence; den > 0 {
		m.EvidenceValidityRate = divOr0(m.EvidenceValidFindings, den)
	} else if m.FindingsTotal > 0 {
		m.EvidenceValidityRate = 0 // all findings have no evidence: nothing to verify
	}
	matchedValid := 0
	for _, mm := range mr.Matched {
		if mm.Actual.EvidenceState == "valid" {
			matchedValid++
		}
	}
	m.FindingEvidenceCoverage = divOr0(matchedValid, expectedCount)
	return m
}

func divOr0(num, den int) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func divOr0f(num, den float64) float64 {
	if den <= 0 {
		return 0
	}
	return num / den
}

// TimingStats is a duration distribution summary (§E1).
type TimingStats struct {
	Count   int             `json:"count"`
	Mean    float64         `json:"mean_ms"`
	Median  float64         `json:"median_ms"`
	P90     float64         `json:"p90_ms"`
	P95     float64         `json:"p95_ms"`
	Min     float64         `json:"min_ms"`
	Max     float64         `json:"max_ms"`
	Samples []time.Duration `json:"-"`
}

func computeTimingStats(durations []time.Duration) TimingStats {
	out := TimingStats{Count: len(durations)}
	if len(durations) == 0 {
		return out
	}
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum float64
	for _, d := range sorted {
		sum += ms(d)
	}
	out.Mean = sum / float64(len(sorted))
	out.Min = ms(sorted[0])
	out.Max = ms(sorted[len(sorted)-1])
	out.Median = ms(sorted[len(sorted)/2])
	pct := func(p float64) float64 {
		idx := int(p*float64(len(sorted)-1) + 0.5)
		return ms(sorted[idx])
	}
	out.P90 = pct(0.90)
	out.P95 = pct(0.95)
	return out
}

// Timings is the §E1 timing breakdown. Sources are existing ARTEX telemetry
// (activity timestamps + llm_usage latencies); observations are never injected.
type Timings struct {
	TotalTaskDuration     TimingStats `json:"total_task_duration"`
	PlannerModelTime      TimingStats `json:"planner_model_time"`
	WorkerModelTime       TimingStats `json:"worker_model_time"`
	ToolExecutionTime     TimingStats `json:"tool_execution_time"`
	EvidenceValidationTime TimingStats `json:"evidence_validation_time"`
	FindingValidationTime  TimingStats `json:"finding_validation_time"`
}

// ModelUsage is the §E2/§E3 consumption/cost view.
type ModelUsage struct {
	Calls             int            `json:"calls"`
	CallsPerLane      map[string]int `json:"calls_per_lane,omitempty"`
	InputTokens       int            `json:"input_tokens"`
	OutputTokens      int            `json:"output_tokens"`
	TotalTokens       int            `json:"total_tokens"`
	CacheReadTokens   int            `json:"cache_read_tokens"`
	CacheWriteTokens  int            `json:"cache_write_tokens"`
	ProviderErrors    int            `json:"provider_errors"`
	Model             string         `json:"model,omitempty"`
	Profile           string         `json:"profile_name,omitempty"`
	UsageMetadataAvailable bool      `json:"usage_metadata_available"`
}

// Reliability is the §E4 reliability view.
type Reliability struct {
	TasksCompleted               int      `json:"tasks_completed"`
	TasksFailed                  int      `json:"tasks_failed"`
	TasksTimedOut                int      `json:"tasks_timed_out"`
	TasksCancelled               int      `json:"tasks_cancelled"`
	IncompleteResults            int      `json:"incomplete_results"`
	IncompleteCases              []string `json:"incomplete_cases,omitempty"`
	ToolInvocations              int      `json:"tool_invocations"`
	ToolFailures                 int      `json:"tool_failures"`
	EvidenceValidationFailures   int      `json:"evidence_validation_failures"`
	Errors                       []string `json:"errors,omitempty"`
}

// CaseMetrics is per-case metrics + status + timing + usage (§C4, §D3).
type CaseMetrics struct {
	CaseID                    string         `json:"case_id"`
	TaskID                    string         `json:"task_id,omitempty"`
	Mode                      string         `json:"mode,omitempty"`
	Status                    string         `json:"status"` // completed | timed_out | cancelled | execution_error | incomplete
	CM                        ConfusionMatrix `json:"cm"`
	Metrics                   Metrics        `json:"metrics"`
	MatchResult               MatchResult    `json:"match_result"`
	ActualFindings            []ActualFinding `json:"actual_findings"`
	ToolInvocations           int            `json:"tool_invocations"`
	ToolFailures              int            `json:"tool_failures"`
	EvidenceValidationFailures int           `json:"evidence_validation_failures"`
	Errors                    []string       `json:"errors,omitempty"`
	Timings                   Timings        `json:"timings"`
	ModelUsage                ModelUsage     `json:"model_usage"`
	// Dedup makes duplicate-handling outcomes visible separately from detection
	// metrics (IX.2.1 §F2). A lower raw finding count is never reported as an
	// accuracy improvement on its own.
	Dedup                     DedupMetrics   `json:"dedup"`
}

// DedupMetrics is the per-case delta of the finding dedup lifecycle counters.
type DedupMetrics struct {
	Submissions         uint64 `json:"submissions"`
	New                 uint64 `json:"new"`
	Duplicates          uint64 `json:"duplicates"`
	EvidenceAttachments uint64 `json:"evidence_attachments"`
	Conflicts           uint64 `json:"conflicts"`
	Errors              uint64 `json:"errors"`
}

// SuiteMetrics is the §D3 suite-level metrics view.
type SuiteMetrics struct {
	SuiteID           string        `json:"suite_id"`
	Mode              string        `json:"mode"`
	Version           string        `json:"version"`
	Cases             []CaseMetrics `json:"cases"`
	Micro             Metrics       `json:"micro"`
	Macro             MacroMetrics  `json:"macro"`
	Reliability       Reliability   `json:"reliability"`
	ModelUsage        ModelUsage    `json:"model_usage"`
	Timings           Timings       `json:"timings"`
	CaseCompletionCount int         `json:"case_completion_count"`
}

type MacroMetrics struct {
	Cases          int     `json:"cases"`
	Precision      float64 `json:"precision"`
	Recall         float64 `json:"recall"`
	F1             float64 `json:"f1"`
	Specificity    float64 `json:"specificity"`
	CompletedCases int     `json:"completed_cases"`
}

func ComputeSuiteMetrics(suite *Suite, results []CaseMetrics, mode string) SuiteMetrics {
	out := SuiteMetrics{SuiteID: suite.SuiteID, Cases: results, Mode: mode}
	if out.Mode == "" && len(results) > 0 {
		out.Mode = results[0].Mode
	}
	var tot ConfusionMatrix
	var prec, rec, f1, spec float64
	completed := 0
	var matches []FindingMatch
	var actuals []ActualFinding
	var totalDur, planDur, workDur, toolDur, evDur, findDur []time.Duration
	var inputTokens, outputTokens, cacheRead, cacheWrite, calls, providerErrors int
	for _, c := range results {
		tot.TP += c.CM.TP; tot.FP += c.CM.FP; tot.TN += c.CM.TN; tot.FN += c.CM.FN
		if c.Status == "completed" {
			completed++
		}
		m := c.Metrics
		prec += m.Precision; rec += m.Recall; f1 += m.F1; spec += m.Specificity
		matches = append(matches, c.MatchResult.Matched...)
		actuals = append(actuals, c.ActualFindings...)
		totalDur = append(totalDur, c.Timings.TotalTaskDuration.Samples...)
		planDur = append(planDur, c.Timings.PlannerModelTime.Samples...)
		workDur = append(workDur, c.Timings.WorkerModelTime.Samples...)
		toolDur = append(toolDur, c.Timings.ToolExecutionTime.Samples...)
		evDur = append(evDur, c.Timings.EvidenceValidationTime.Samples...)
		findDur = append(findDur, c.Timings.FindingValidationTime.Samples...)
		calls += c.ModelUsage.Calls
		inputTokens += c.ModelUsage.InputTokens
		outputTokens += c.ModelUsage.OutputTokens
		cacheRead += c.ModelUsage.CacheReadTokens
		cacheWrite += c.ModelUsage.CacheWriteTokens
		providerErrors += c.ModelUsage.ProviderErrors
		out.Reliability.ToolInvocations += c.ToolInvocations
		out.Reliability.ToolFailures += c.ToolFailures
		out.Reliability.EvidenceValidationFailures += c.EvidenceValidationFailures
		out.Reliability.Errors = append(out.Reliability.Errors, c.Errors...)
		if c.Status != "completed" {
			out.Reliability.IncompleteResults++
			out.Reliability.IncompleteCases = append(out.Reliability.IncompleteCases, c.CaseID)
		} else {
			out.Reliability.TasksCompleted++
		}
		if c.Status == "execution_error" {
			out.Reliability.TasksFailed++
		}
		if c.Status == "timed_out" {
			out.Reliability.TasksTimedOut++
		}
	}
	out.Micro = ComputeMetrics(tot, MatchResult{Matched: matches}, actuals, sumExpected(results))
	if len(results) > 0 {
		out.Micro.CaseCompletionRate = divOr0(completed, len(results))
	}
	n := float64(len(results))
	if n > 0 {
		out.Macro = MacroMetrics{Cases: len(results), CompletedCases: completed,
			Precision: prec / n, Recall: rec / n, F1: f1 / n, Specificity: spec / n}
	}
	out.CaseCompletionCount = completed
	out.ModelUsage = ModelUsage{Calls: calls, InputTokens: inputTokens, OutputTokens: outputTokens,
		CacheReadTokens: cacheRead, CacheWriteTokens: cacheWrite, ProviderErrors: providerErrors,
		UsageMetadataAvailable: calls > 0}
	out.Timings = Timings{
		TotalTaskDuration:     computeTimingStats(totalDur),
		PlannerModelTime:      computeTimingStats(planDur),
		WorkerModelTime:       computeTimingStats(workDur),
		ToolExecutionTime:     computeTimingStats(toolDur),
		EvidenceValidationTime: computeTimingStats(evDur),
		FindingValidationTime:  computeTimingStats(findDur),
	}
	return out
}

func sumExpected(results []CaseMetrics) int {
	s := 0
	for _, c := range results {
		s += c.Metrics.ExpectedFindings
	}
	return s
}
