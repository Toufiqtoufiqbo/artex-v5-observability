package benchmark

import "time"

// suiteV5HardeningV924 adds detection-hardening cases to the existing
// benchmark corpus. No new offensive capability: cases only exercise the
// EXISTING detection/finding pipeline with scripted (offline) provider turns.
// Ground truth is authored here, independent of ARTEX output (§11).
const hardeningSuiteID = "artex-v5-hardening-v924"

var suiteV5HardeningV924 = &Suite{
	SuiteID:      hardeningSuiteID,
	SuiteVersion: SchemaVersion,
	Description:  "IX.2.4 hardening suite: parameter-level finding distinction, endpoint-level distinction, negative no-proof case, repeat-dedup and mixed vuln classes. Offline scripted provider through the real Engine/Worker pipeline; not a claim about real-LLM accuracy.",
	CreatedAt:    time.Date(2026, 11, 24, 16, 0, 0, 0, time.UTC),
	Cases:        datasetV5Hardening(),
}

func init() { DefaultSuites = append(DefaultSuites, suiteV5HardeningV924) }

func datasetV5Hardening() []*Case {
	const fix = "fixture-v5-local-2026-11-23"
	const gt = "gt-hardening-v1-2026-11-24"
	base := func(id, category, desc string, expected []ExpectedFinding) *Case {
		return &Case{
			ID: id, SuiteID: hardeningSuiteID, SchemaVersion: SchemaVersion,
			Category: category, Description: desc, FixtureVersion: fix,
			ReviewStatus: "reviewed", GroundTruthVersion: gt,
			ExpectedFindings: expected,
			ExecutionLimits:  ExecutionLimits{TimeoutSeconds: 60, MaxIntents: 1},
		}
	}
	return []*Case{
		func() *Case {
			c := base("hardening_sqli_two_parameters", "detection_positive",
				"Two distinct SQLi findings (id vs sort parameter) must remain distinct: TP=2.",
				[]ExpectedFinding{
					{VulnClass: "SQLi", Name: "Blind SQL Injection (id)", Severity: "high"},
					{VulnClass: "SQLi", Name: "Blind SQL Injection (sort)", Severity: "high"},
				})
			c.Script = []ScriptResponse{
				reportFindingScript("SQLi", "Blind SQL Injection (id)", "high", "布尔盲注 id 参数", "触发：`GET /v?id=1' AND 1=1` 返回 HTTP/1.1 200，真假响应体不同", "http_diff"),
				reportFindingScript("SQLi", "Blind SQL Injection (sort)", "high", "布尔盲注 sort 参数", "触发：`GET /v?sort=1' AND 1=1` 返回 HTTP/1.1 200，真假响应体不同", "http_diff"),
				textScript("已结束。"),
			}
			return c
		}(),
		func() *Case {
			c := base("hardening_xss_two_endpoints", "detection_positive",
				"Two distinct reflected-XSS findings on different endpoints must remain distinct: TP=2.",
				[]ExpectedFinding{
					{VulnClass: "XSS", Name: "Reflected XSS (search)", Severity: "medium"},
					{VulnClass: "XSS", Name: "Reflected XSS (profile)", Severity: "medium"},
				})
			c.Script = []ScriptResponse{
				reportFindingScript("XSS", "Reflected XSS (search)", "medium", "search 端点反射", "触发：`GET /search?q=<b>x</b>` HTTP/1.1 200，响应体含未转义 payload", "http_diff"),
				reportFindingScript("XSS", "Reflected XSS (profile)", "medium", "profile 端点反射", "触发：`GET /profile?u=<i>x</i>` HTTP/1.1 200，响应体含未转义 payload", "http_diff"),
				textScript("已结束。"),
			}
			return c
		}(),
		func() *Case {
			c := base("hardening_unconfirmed_no_finding", "detection_negative",
				"No reproducible proof → no finding (TN, no FP).",
				nil)
			c.Script = []ScriptResponse{textScript("仅观察到普通 404，无漏洞证据。")}
			return c
		}(),
		func() *Case {
			c := base("hardening_repeat_evidence_dedup", "lifecycle_edge",
				"Same finding reported twice with additional evidence: dedup to one logical finding (TP=1).",
				[]ExpectedFinding{{VulnClass: "SQLi", Name: "Blind SQL Injection (id)", Severity: "high"}})
			c.Script = []ScriptResponse{
				reportFindingScript("SQLi", "Blind SQL Injection (id)", "high", "首次观测", "触发：`GET /v?id=1' AND 1=1` HTTP/1.1 200，布尔差分成立", "http_diff"),
				reportFindingScript("SQLi", "Blind SQL Injection (id)", "high", "补充证据", "触发：`GET /v?id=1' AND 1=0` HTTP/1.1 200，假分支响应体不同，布尔差分成立", "http_diff"),
				textScript("已结束。"),
			}
			return c
		}(),
		func() *Case {
			c := base("hardening_mixed_vuln_classes", "detection_positive",
				"Different vulnerability classes on one host must remain distinct: TP=2.",
				[]ExpectedFinding{
					{VulnClass: "SQLi", Name: "Blind SQL Injection (login)", Severity: "high"},
					{VulnClass: "SSTI", Name: "Server-Side Template Injection (eval)", Severity: "high"},
				})
			c.Script = []ScriptResponse{
				reportFindingScript("SQLi", "Blind SQL Injection (login)", "high", "登录参数注入", "触发：`POST /login` 参数 user 注入后 HTTP/1.1 200 真假差分成立", "http_diff"),
				reportFindingScript("SSTI", "Server-Side Template Injection (eval)", "high", "模板表达式求值", "触发：`GET /tpl?name=7*7` HTTP/1.1 200，响应体含 49", "http_diff"),
				textScript("已结束。"),
			}
			return c
		}(),
		func() *Case {
			c := base("hardening_traversal_confirmed", "detection_positive",
				"Path traversal confirmed with a reproducible artifact (TP=1).",
				[]ExpectedFinding{{VulnClass: "Traversal", Name: "Path Traversal (etc/passwd)", Severity: "medium"}})
			c.Script = []ScriptResponse{
				reportFindingScript("Traversal", "Path Traversal (etc/passwd)", "medium", "遍历读取系统文件", "触发：`GET /download?file=../../etc/passwd` HTTP/1.1 200，响应体含 root:x:0:0: 行", "http_diff"),
				textScript("已结束。"),
			}
			return c
		}(),
		func() *Case {
			c := base("hardening_error_sqli_distinct", "detection_positive",
				"Error-based SQLi is a distinct finding from boolean SQLi (TP=1).",
				[]ExpectedFinding{{VulnClass: "SQLi", Name: "Error-Based SQL Injection", Severity: "high"}})
			c.Script = []ScriptResponse{
				reportFindingScript("SQLi", "Error-Based SQL Injection", "high", "报错注入", "触发：`GET /v?id=1'` HTTP/1.1 500，响应体含 SQL syntax near 原始数据库报错", "http_diff"),
				textScript("已结束。"),
			}
			return c
		}(),
		func() *Case {
			c := base("hardening_idor_no_proof", "detection_negative",
				"Suspected IDOR without reproduced cross-user access produces no finding (no FP).",
				nil)
			c.Script = []ScriptResponse{textScript("只观察到参数可枚举，但未拿到跨用户访问的复现证据。")}
			return c
		}(),
	}
}
