package benchmark

import "time"

// suiteV5DedupV921 is the IX.2.1 duplicate-handling suite. Ground truth is
// authored independently: for each case we assert the number and identity of
// LOGICAL findings that should exist after deduplication, plus the expected
// duplicate-submission count. It is deliberately small and is not presented as
// universal detection accuracy.
const dedupSuiteID = "artex-v5-dedup-v921"

var suiteV5DedupV921 = &Suite{
	SuiteID:      dedupSuiteID,
	SuiteVersion: SchemaVersion,
	Description:  "IX.2.1 finding dedup/idempotency suite: repeated reports, retries, distinct findings and invalid evidence after an existing finding. Run through the real engine with the scripted offline provider.",
	CreatedAt:    time.Date(2026, 11, 23, 18, 0, 0, 0, time.UTC),
	Cases:        datasetV5Dedup(),
}

func init() { DefaultSuites = append(DefaultSuites, suiteV5DedupV921) }

func datasetV5Dedup() []*Case {
	const fix = "fixture-v5-local-2026-11-23"
	const gt = "gt-dedup-v1-2026-11-23"
	base := func(id, desc string, expected []ExpectedFinding, script []ScriptResponse) *Case {
		return &Case{
			ID: id, SuiteID: dedupSuiteID, SchemaVersion: SchemaVersion,
			Category: "lifecycle_edge", Description: desc,
			FixtureVersion: fix, ReviewStatus: "reviewed", GroundTruthVersion: gt,
			ExpectedFindings: expected,
			ExecutionLimits:  ExecutionLimits{TimeoutSeconds: 60, MaxIntents: 1},
			Script:           script,
		}
	}
	return []*Case{
		base("dedup_exact_repeat",
			"同一 finding 连续上报两次 → 1 条逻辑结果，1 次重复观测。",
			[]ExpectedFinding{{VulnClass: "SQLi", Name: "Blind SQL Injection (boolean)", Severity: "high"}},
			[]ScriptResponse{
				reportFindingScript("SQLi", "Blind SQL Injection (boolean)", "high", "first", "GET /vuln?id=1 → HTTP/1.1 200 OK payload reflected", "http_diff"),
				reportFindingScript("SQLi", "Blind SQL Injection (boolean)", "high", "second observation", "GET /vuln?id=1 → HTTP/1.1 200 OK payload reflected", "http_diff"),
				textScript("done"),
			}),
		base("dedup_retry_three_times",
			"不确定响应后重试 3 次 → 仍为 1 条逻辑结果（幂等）。",
			[]ExpectedFinding{{VulnClass: "SSTI", Name: "Server-Side Template Injection (eval)", Severity: "high"}},
			[]ScriptResponse{
				reportFindingScript("SSTI", "Server-Side Template Injection (eval)", "high", "attempt 1", "POST /tpl HTTP/1.1 200 OK 49 → 7 bytes delta", "http_diff"),
				reportFindingScript("SSTI", "Server-Side Template Injection (eval)", "high", "attempt 2", "POST /tpl HTTP/1.1 200 OK 49 → 7 bytes delta", "http_diff"),
				reportFindingScript("SSTI", "Server-Side Template Injection (eval)", "high", "attempt 3", "POST /tpl HTTP/1.1 200 OK 49 → 7 bytes delta", "http_diff"),
				textScript("done"),
			}),
		base("dedup_additional_evidence",
			"同一 finding 附带新的可验证证据 → 1 条结果，证据被保留而非丢弃。",
			[]ExpectedFinding{{VulnClass: "XSS", Name: "Reflected XSS (payload echo)", Severity: "medium"}},
			[]ScriptResponse{
				reportFindingScript("XSS", "Reflected XSS (payload echo)", "medium", "first", "GET /?q=<b> → payload reflected in body", "response_body"),
				reportFindingScript("XSS", "Reflected XSS (payload echo)", "medium", "second", "GET /?q=<i> → HTTP/1.1 200 OK payload reflected again", "http_diff"),
				textScript("done"),
			}),
		base("dedup_distinct_parameters",
			"同一端点、不同受影响参数 → 2 条独立结果，不得被合并。",
			[]ExpectedFinding{
				{VulnClass: "SQLi", Name: "Blind SQL Injection (id)", Severity: "high"},
				{VulnClass: "SQLi", Name: "Blind SQL Injection (sort)", Severity: "high"},
			},
			[]ScriptResponse{
				reportFindingScript("SQLi", "Blind SQL Injection (id)", "high", "param id", "GET /vuln?id=1' → HTTP/1.1 200 OK delta", "http_diff"),
				reportFindingScript("SQLi", "Blind SQL Injection (sort)", "high", "param sort", "GET /vuln?sort=1' → HTTP/1.1 200 OK delta", "http_diff"),
				textScript("done"),
			}),
		base("dedup_distinct_vuln_classes",
			"同一资源上两个不同漏洞类别 → 2 条独立结果。",
			[]ExpectedFinding{
				{VulnClass: "SQLi", Name: "Blind SQL Injection (boolean)", Severity: "high"},
				{VulnClass: "XSS", Name: "Reflected XSS (payload echo)", Severity: "medium"},
			},
			[]ScriptResponse{
				reportFindingScript("SQLi", "Blind SQL Injection (boolean)", "high", "sqli", "GET /v?id=1 → HTTP/1.1 200 OK delta", "http_diff"),
				reportFindingScript("XSS", "Reflected XSS (payload echo)", "medium", "xss", "GET /?q=<b> → payload reflected", "response_body"),
				textScript("done"),
			}),
		base("dedup_invalid_evidence_after_existing",
			"已存在有效 finding 后提交未验证证据 → 证据门拒绝，不新增也不篡改结果。",
			[]ExpectedFinding{{VulnClass: "SQLi", Name: "Blind SQL Injection (boolean)", Severity: "high"}},
			[]ScriptResponse{
				reportFindingScript("SQLi", "Blind SQL Injection (boolean)", "high", "valid", "GET /v?id=1 → HTTP/1.1 200 OK reflect", "http_diff"),
				reportFindingScript("SQLi", "Blind SQL Injection (boolean)", "high", "invalid", "not confirmed: no successful injection observed", "http_diff"),
				textScript("done"),
			}),
	}
}
