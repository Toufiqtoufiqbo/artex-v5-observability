package benchmark

import (
	"fmt"
	"strings"
	"time"
)

// Phase IX.2.2 — planning benchmark suite.
//
// The shipped IX.1/IX.2.1 suites seed intents directly (runner.go:118-127), so
// the planner is never the measured surface. This suite measures intent
// planning itself: duplicate collapse, preservation of genuinely distinct
// work, dependency gating and failure propagation.
//
// Ground truth is authored here, independently of ARTEX output: for each case
// we declare how many LOGICAL intents exist and which proposal indices must
// collapse / must stay distinct. A proposal is one add_intent item the scripted
// planner submits; it is not an assertion about the model's judgement.

const planningSuiteID = "artex-v5-planning-v922"

// PlanningProposal is one intent the scripted planner submits.
type PlanningProposal struct {
	Summary   string `json:"summary"`
	Operation string `json:"operation,omitempty"`
	Resource  string `json:"resource,omitempty"`
	DependsOn []int  `json:"depends_on,omitempty"` // proposal indices, resolved to node ids
	RetestOf  int    `json:"retest_of,omitempty"`  // proposal index
	Priority  int    `json:"priority,omitempty"`
}

// PlanningRound is one Planner.Plan round. Each round is a fresh session, so
// cross-round cases measure dedup against intents created by earlier rounds.
type PlanningRound struct {
	Proposals []PlanningProposal `json:"proposals"`
	Batch     bool               `json:"batch,omitempty"` // submit as one add_intent batch
	// Complete marks the just-created intents done before the next round
	// (models a worker finishing them; needed for retest/repeat cases).
	Complete bool `json:"complete,omitempty"`
	// FailPrerequisites marks the referenced proposal indices blocked and runs
	// dependency propagation before the next round.
	FailPrerequisites []int `json:"fail_prerequisites,omitempty"`
}

// PlanningPair ties a dependent proposal to its prerequisite proposal.
type PlanningPair struct {
	Dependent    int `json:"dependent"`
	Prerequisite int `json:"prerequisite"`
}

// PlanningCase is one reviewed planning scenario.
type PlanningCase struct {
	ID             string `json:"case_id"`
	SuiteID        string `json:"suite_id"`
	Description    string `json:"description"`
	Category       string `json:"category"` // planning_duplicate | planning_distinct | planning_dependency
	ReviewStatus   string `json:"review_status"`
	GroundTruthVersion string `json:"ground_truth_version"`
	Rounds         []PlanningRound `json:"rounds"`
	// ExpectedLogicalIntents is the ground-truth number of distinct units of
	// work across all rounds (retests/repeats count as new attempts).
	ExpectedLogicalIntents int `json:"expected_logical_intents"`
	// DuplicateGroups: proposal indices (global, round-major) that MUST share
	// one node id.
	DuplicateGroups [][]int `json:"duplicate_groups,omitempty"`
	// DistinctGroups: proposal indices that MUST NOT share a node id.
	DistinctGroups [][]int `json:"distinct_groups,omitempty"`
	// GatedPairs: dependent must not be claimable until prerequisite is done.
	GatedPairs []PlanningPair `json:"gated_pairs,omitempty"`
	// FailPairs: when the prerequisite is marked blocked, the dependent must
	// become blocked (propagation), not remain open-but-unclaimable.
	FailPairs []PlanningPair `json:"fail_pairs,omitempty"`
	// ExpectedBlockedDependents is the number of intents the propagation step
	// must block (transitive dependents included).
	ExpectedBlockedDependents int `json:"expected_blocked_dependents,omitempty"`
	// ExpectedRetestEdges is the minimum number of retest_of edges the case
	// must produce (new attempts after completion).
	ExpectedRetestEdges int `json:"expected_retest_edges,omitempty"`
}

// PlanningSuite is a versioned planning benchmark suite.
type PlanningSuite struct {
	SuiteID      string          `json:"suite_id"`
	SuiteVersion string          `json:"suite_version"`
	Description  string          `json:"description"`
	CreatedAt    time.Time       `json:"created_at"`
	Cases        []*PlanningCase `json:"cases"`
}

var planningSuites = []*PlanningSuite{suiteV5PlanningV922}

func PlanningSuiteByID(id string) *PlanningSuite {
	for _, s := range planningSuites {
		if s.SuiteID == id {
			return s
		}
	}
	return nil
}

func AllPlanningSuiteIDs() []string {
	out := make([]string, 0, len(planningSuites))
	for _, s := range planningSuites {
		out = append(out, s.SuiteID)
	}
	return out
}

func (s *PlanningSuite) Validate() []string {
	var errs []string
	if strings.TrimSpace(s.SuiteID) == "" {
		errs = append(errs, "planning suite: suite_id empty")
	}
	if s.SuiteVersion != SchemaVersion {
		errs = append(errs, fmt.Sprintf("planning suite %s: unsupported suite_version %q", s.SuiteID, s.SuiteVersion))
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		if c == nil {
			errs = append(errs, fmt.Sprintf("planning suite %s: nil case", s.SuiteID))
			continue
		}
		if seen[c.ID] {
			errs = append(errs, fmt.Sprintf("planning suite %s: duplicate case id %q", s.SuiteID, c.ID))
		}
		seen[c.ID] = true
		errs = append(errs, c.validate()...)
	}
	return errs
}

func (c *PlanningCase) totalProposals() int {
	n := 0
	for _, r := range c.Rounds {
		n += len(r.Proposals)
	}
	return n
}

func (c *PlanningCase) validate() []string {
	var errs []string
	if strings.TrimSpace(c.ID) == "" {
		errs = append(errs, "planning case: case_id empty")
	}
	if strings.TrimSpace(c.Description) == "" {
		errs = append(errs, fmt.Sprintf("planning case %s: description empty", c.ID))
	}
	switch c.Category {
	case "planning_duplicate", "planning_distinct", "planning_dependency":
	default:
		errs = append(errs, fmt.Sprintf("planning case %s: invalid category %q", c.ID, c.Category))
	}
	switch c.ReviewStatus {
	case "reviewed", "pending", "excluded":
	default:
		errs = append(errs, fmt.Sprintf("planning case %s: invalid review_status %q", c.ID, c.ReviewStatus))
	}
	total := c.totalProposals()
	if total == 0 {
		errs = append(errs, fmt.Sprintf("planning case %s: no proposals", c.ID))
	}
	if c.ExpectedLogicalIntents <= 0 {
		errs = append(errs, fmt.Sprintf("planning case %s: expected_logical_intents must be > 0", c.ID))
	}
	check := func(label string, groups [][]int) {
		for _, g := range groups {
			if len(g) < 2 {
				errs = append(errs, fmt.Sprintf("planning case %s: %s group needs >=2 indices", c.ID, label))
			}
			for _, i := range g {
				if i < 0 || i >= total {
					errs = append(errs, fmt.Sprintf("planning case %s: %s index %d out of range [0,%d)", c.ID, label, i, total))
				}
			}
		}
	}
	check("duplicate", c.DuplicateGroups)
	check("distinct", c.DistinctGroups)
	pairs := func(label string, ps []PlanningPair) {
		for _, p := range ps {
			if p.Dependent < 0 || p.Dependent >= total || p.Prerequisite < 0 || p.Prerequisite >= total {
				errs = append(errs, fmt.Sprintf("planning case %s: %s pair (%d,%d) out of range", c.ID, label, p.Dependent, p.Prerequisite))
			}
			if p.Dependent == p.Prerequisite {
				errs = append(errs, fmt.Sprintf("planning case %s: %s pair self-referential", c.ID, label))
			}
		}
	}
	pairs("gated", c.GatedPairs)
	pairs("fail", c.FailPairs)
	return errs
}

var suiteV5PlanningV922 = &PlanningSuite{
	SuiteID:      planningSuiteID,
	SuiteVersion: SchemaVersion,
	Description:  "IX.2.2 intent-management suite: duplicate collapse, preservation of distinct work, dependency gating and failure propagation, measured on the real Planner.Plan path with a scripted provider. Not a statistical claim about LLM planning quality.",
	CreatedAt:    time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC),
	Cases:        datasetV5Planning(),
}

func datasetV5Planning() []*PlanningCase {
	const gt = "gt-planning-v1-2026-11-24"
	base := func(id, category, desc string) *PlanningCase {
		return &PlanningCase{
			ID: id, SuiteID: planningSuiteID, Category: category,
			Description: desc, ReviewStatus: "reviewed", GroundTruthVersion: gt,
		}
	}
	return []*PlanningCase{
		func() *PlanningCase {
			c := base("plan_exact_repeat", "planning_duplicate",
				"The planner submits the same intent in two consecutive rounds; it must resolve to one pending intent.")
			c.Rounds = []PlanningRound{
				{Proposals: []PlanningProposal{{Summary: "对 /login 做 SQL 注入测试"}}},
				{Proposals: []PlanningProposal{{Summary: "对 /login 做 SQL 注入测试"}}},
			}
			c.ExpectedLogicalIntents = 1
			c.DuplicateGroups = [][]int{{0, 1}}
			return c
		}(),
		func() *PlanningCase {
			c := base("plan_paraphrase_structured", "planning_duplicate",
				"Two different wordings of the same operation on the same resource must collapse to one intent.")
			c.Rounds = []PlanningRound{{Proposals: []PlanningProposal{
				{Summary: "测试 /login 的布尔盲注", Operation: "sqli-test", Resource: "/login?id=1"},
				{Summary: "SQL injection check on the login endpoint", Operation: "sqli-test", Resource: "/login?id=1"},
			}}}
			c.ExpectedLogicalIntents = 1
			c.DuplicateGroups = [][]int{{0, 1}}
			return c
		}(),
		func() *PlanningCase {
			c := base("plan_distinct_parameters", "planning_distinct",
				"Same operation on different query parameters is different work and must not be merged.")
			c.Rounds = []PlanningRound{{Proposals: []PlanningProposal{
				{Summary: "IDOR on user id", Operation: "idor-test", Resource: "/api/user?id=1"},
				{Summary: "IDOR on user id", Operation: "idor-test", Resource: "/api/user?id=2"},
			}}}
			c.ExpectedLogicalIntents = 2
			c.DistinctGroups = [][]int{{0, 1}}
			return c
		}(),
		func() *PlanningCase {
			c := base("plan_distinct_operations", "planning_distinct",
				"Different operations on the same resource are different work.")
			c.Rounds = []PlanningRound{{Proposals: []PlanningProposal{
				{Summary: "SQL injection on /upload", Operation: "sqli-test", Resource: "/upload"},
				{Summary: "Upload bypass on /upload", Operation: "upload-bypass", Resource: "/upload"},
			}}}
			c.ExpectedLogicalIntents = 2
			c.DistinctGroups = [][]int{{0, 1}}
			return c
		}(),
		func() *PlanningCase {
			c := base("plan_batch_mixed", "planning_duplicate",
				"One batch contains a duplicate and two distinct directions: 3 logical intents, 4 submissions.")
			c.Rounds = []PlanningRound{{Batch: true, Proposals: []PlanningProposal{
				{Summary: "枚举 /api 端点"},
				{Summary: "枚举 /api 端点"},
				{Summary: "测试 /upload 文件上传绕过"},
				{Summary: "检测 /admin 默认口令"},
			}}}
			c.ExpectedLogicalIntents = 3
			c.DuplicateGroups = [][]int{{0, 1}}
			c.DistinctGroups = [][]int{{0, 2}, {0, 3}, {2, 3}}
			return c
		}(),
		func() *PlanningCase {
			c := base("plan_retest_after_completion", "planning_duplicate",
				"After an intent completes, re-proposing it creates one new attempt linked as retest_of, not a silent suppression.")
			c.Rounds = []PlanningRound{
				{Proposals: []PlanningProposal{{Summary: "测试上传绕过", Operation: "upload-bypass", Resource: "/upload"}}, Complete: true},
				{Proposals: []PlanningProposal{{Summary: "复测上传绕过（修复后）", Operation: "upload-bypass", Resource: "/upload"}}},
			}
			c.ExpectedLogicalIntents = 2
			c.DistinctGroups = [][]int{{0, 1}}
			c.ExpectedRetestEdges = 1
			return c
		}(),
		func() *PlanningCase {
			c := base("plan_dependency_gate", "planning_dependency",
				"A dependent intent is not claimable until its prerequisite is done.")
			c.Rounds = []PlanningRound{{Proposals: []PlanningProposal{
				{Summary: "先拿到登录会话"},
				{Summary: "用会话访问 /admin", DependsOn: []int{0}},
			}}}
			c.ExpectedLogicalIntents = 2
			c.DistinctGroups = [][]int{{0, 1}}
			c.GatedPairs = []PlanningPair{{Dependent: 1, Prerequisite: 0}}
			return c
		}(),
		func() *PlanningCase {
			c := base("plan_dependency_failure", "planning_dependency",
				"A failed prerequisite blocks its dependents instead of leaving them open-but-unclaimable.")
			c.Rounds = []PlanningRound{{Proposals: []PlanningProposal{
				{Summary: "先拿到登录会话"},
				{Summary: "用会话访问 /admin", DependsOn: []int{0}},
				{Summary: "用会话访问 /billing", DependsOn: []int{1}},
			}}}
			c.ExpectedLogicalIntents = 3
			c.DistinctGroups = [][]int{{0, 1}, {0, 2}, {1, 2}}
			c.FailPairs = []PlanningPair{{Dependent: 1, Prerequisite: 0}}
			c.ExpectedBlockedDependents = 2
			return c
		}(),
	}
}
