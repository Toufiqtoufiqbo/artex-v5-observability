package benchmark

import (
	"encoding/json"
	"time"
)

// dataset.go — the initial benchmark dataset (§B3): a small, carefully reviewed
// corpus of positive, negative and edge cases. Ground truth is authored here by
// the benchmark, independent of ARTEX output (§11).

var suiteV5OfflineBaseline = &Suite{
	SuiteID:      "artex-v5-offline-baseline",
	SuiteVersion: SchemaVersion,
	Description:  "Offline deterministic baseline suite (Phase IX.1): scripted positive cases through the real Planner/Worker, negative cases and lifecycle edge cases. Not a statistical generalization — an engineering baseline for IX.2 comparison.",
	CreatedAt:    time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC),
	Cases:        datasetV5OfflineBaseline(),
}

// DefaultSuites is the registry of shipped suites (§H1: `artex benchmark list`).
var DefaultSuites = []*Suite{suiteV5OfflineBaseline}

func SuiteByID(id string) *Suite {
	for _, s := range DefaultSuites {
		if s.SuiteID == id {
			return s
		}
	}
	return nil
}

func AllSuiteIDs() []string {
	out := make([]string, 0, len(DefaultSuites))
	for _, s := range DefaultSuites {
		out = append(out, s.SuiteID)
	}
	return out
}

// --- helpers for building scripted case data ---

func reportFindingScript(vulnclass, name, severity, summary, evidence, proofType string, assetIDs ...int64) ScriptResponse {
	in := map[string]any{
		"vulnclass": vulnclass, "name": name, "severity": severity, "summary": summary,
		"evidence": evidence, "proof_type": proofType,
	}
	if len(assetIDs) > 0 {
		in["asset_ids"] = assetIDs
	}
	raw, _ := json.Marshal(in)
	return ScriptResponse{ToolUses: []ScriptToolUse{{Name: "report_finding", Input: raw}}}
}

func textScript(text string) ScriptResponse { return ScriptResponse{Text: text} }

// --- case corpus ---

func datasetV5OfflineBaseline() []*Case {
	return []*Case{
		{
			ID: "sql_boolean_confirmed_local", SuiteID: "artex-v5-offline-baseline", SchemaVersion: SchemaVersion,
			Category: "detection_positive", Description: "正确地确认：流水线经由真实 report_finding 产出 SQLi boolean 盲注的 TP。",
			FixtureVersion: "fixture-v5-local-2026-11-23", ReviewStatus: "reviewed", GroundTruthVersion: "gt-v1-2026-11-23",
			ExpectedFindings: []ExpectedFinding{{VulnClass: "SQLi", Name: "Blind SQL Injection (boolean)", Severity: "high", EvidenceRequired: true}},
			ExpectedEvidenceRequirements: []EvidenceRequirement{{MatchKey: fingerprintForCase(ExpectedFinding{VulnClass: "SQLi", Name: "Blind SQL Injection (boolean)", Severity: "high"}), MinEvidenceTypes: []string{"baseline"}}},
			ExecutionLimits: ExecutionLimits{TimeoutSeconds: 60, MaxIntents: 1},
			Script: []ScriptResponse{
				reportFindingScript("SQLi", "Blind SQL Injection (boolean)", "high",
					"布尔盲注：布尔真/假两分支在注入时提取可观测的差分",
					"触发：`GET /vuln/sqli?id=1` 返回 `HTTP/1.1 200 OK`，响应体包含 `SELECT` 执行痕迹；通过布尔 oracle，真值/假值响应体不同", "http_diff"),
				textScript("已结束。"),
			},
		},
		{
			ID: "xss_reflected_confirmed_local", SuiteID: "artex-v5-offline-baseline", SchemaVersion: SchemaVersion,
			Category: "detection_positive", Description: "正确地确认：反射 XSS 经由真实 report_finding 产出 TP。",
			FixtureVersion: "fixture-v5-local-2026-11-23", ReviewStatus: "reviewed", GroundTruthVersion: "gt-v1-2026-11-23",
			ExpectedFindings: []ExpectedFinding{{VulnClass: "XSS", Name: "Reflected XSS (payload echo)", Severity: "medium", EvidenceRequired: true}},
			ExpectedEvidenceRequirements: []EvidenceRequirement{{MatchKey: fingerprintForCase(ExpectedFinding{VulnClass: "XSS", Name: "Reflected XSS (payload echo)", Severity: "medium"}), MinEvidenceTypes: []string{"proof"}}},
			ExecutionLimits: ExecutionLimits{TimeoutSeconds: 60, MaxIntents: 1},
			Script: []ScriptResponse{
				reportFindingScript("XSS", "Reflected XSS (payload echo)", "medium",
					"反射 XSS：输入 payload 原样反射进响应体",
					"触发：`GET /vuln/xss?q=<script>alert(1)</script>` 返回 `HTTP/1.1 200 OK`，响应体含完整输入 `<script>alert(1)</script>` 未转义反射", "http_diff"),
				textScript("已结束。"),
			},
		},
		{
			ID: "ssti_confirmed_local", SuiteID: "artex-v5-offline-baseline", SchemaVersion: SchemaVersion,
			Category: "detection_positive", Description: "正确地确认：服务器模板注入（SSTI）经由真实 report_finding 产出 TP。",
			FixtureVersion: "fixture-v5-local-2026-11-23", ReviewStatus: "reviewed", GroundTruthVersion: "gt-v1-2026-11-23",
			ExpectedFindings: []ExpectedFinding{{VulnClass: "SSTI", Name: "Server-Side Template Injection (eval)", Severity: "high", EvidenceRequired: true}},
			ExpectedEvidenceRequirements: []EvidenceRequirement{{MatchKey: fingerprintForCase(ExpectedFinding{VulnClass: "SSTI", Name: "Server-Side Template Injection (eval)", Severity: "high"}), MinEvidenceTypes: []string{"proof"}}},
			ExecutionLimits: ExecutionLimits{TimeoutSeconds: 60, MaxIntents: 1},
			Script: []ScriptResponse{
				reportFindingScript("SSTI", "Server-Side Template Injection (eval)", "high",
					"SSTI：7*7 模板表达式在服务器端求值",
					"触发：`GET /vuln/ssti?name=7*7` 返回 `HTTP/1.1 200 OK`，响应体含 `49`（模板引擎求值结果）", "http_diff"),
				textScript("已结束。"),
			},
		},
		{
			ID: "harmless_404_no_finding", SuiteID: "artex-v5-offline-baseline", SchemaVersion: SchemaVersion,
			Category: "detection_negative", Description: "正确的负例：无异常输出 → 无 finding。",
			FixtureVersion: "fixture-v5-local-2026-11-23", ReviewStatus: "reviewed", GroundTruthVersion: "gt-v1-2026-11-23",
			ExpectedFindings:        nil,
			ExpectedNegativeFindings: nil,
			ExecutionLimits:          ExecutionLimits{TimeoutSeconds: 60, MaxIntents: 1},
			Script:                   []ScriptResponse{textScript("探测完成，未发现异常。")},
		},
		{
			ID: "worker_empty_response_edge", SuiteID: "artex-v5-offline-baseline", SchemaVersion: SchemaVersion,
			Category: "lifecycle_edge", Description: "边界：模型只产生文本、不报 finding → 无 finding（FN=0，FP=0）。",
			FixtureVersion: "fixture-v5-local-2026-11-23", ReviewStatus: "reviewed", GroundTruthVersion: "gt-v1-2026-11-23",
			ExpectedFindings: nil,
			ExecutionLimits:  ExecutionLimits{TimeoutSeconds: 60, MaxIntents: 1},
			Script:           []ScriptResponse{textScript("只记录事实，不报漏洞。")},
		},
		{
			ID: "duplicate_finding_dedup_edge", SuiteID: "artex-v5-offline-baseline", SchemaVersion: SchemaVersion,
			Category: "lifecycle_edge", Description: "边界（§D1）：模型报告同一 finding 两次，按指纹去重后 TP=1，重复不再产生 TP（重复观测计为 FP）。",
			FixtureVersion: "fixture-v5-local-2026-11-23", ReviewStatus: "reviewed", GroundTruthVersion: "gt-v1-2026-11-23",
			ExpectedFindings: []ExpectedFinding{{VulnClass: "SQLi", Name: "Blind SQL Injection (boolean)", Severity: "high"}},
			ExecutionLimits:  ExecutionLimits{TimeoutSeconds: 60, MaxIntents: 1},
			Script: []ScriptResponse{
				reportFindingScript("SQLi", "Blind SQL Injection (boolean)", "high",
					"重复上报", "触发：`GET /vuln/sqli?id=1` 返回 `HTTP/1.1 200 OK`，响应体包含注入痕迹", "http_diff"),
				reportFindingScript("SQLi", "Blind SQL Injection (boolean)", "high",
					"重复上报（第二次）", "触发：`GET /vuln/sqli?id=1` 返回 `HTTP/1.1 200 OK`，响应体包含注入痕迹", "http_diff"),
				textScript("已结束。"),
			},
		},
		{
			ID: "invalid_evidence_gate_edge", SuiteID: "artex-v5-offline-baseline", SchemaVersion: SchemaVersion,
			Category: "lifecycle_edge", Description: "边界：report_finding 被引擎证据门拦截（无可验证产物）→ 无 finding 落库。",
			FixtureVersion: "fixture-v5-local-2026-11-23", ReviewStatus: "reviewed", GroundTruthVersion: "gt-v1-2026-11-23",
			ExpectedFindings: nil,
			ExecutionLimits:  ExecutionLimits{TimeoutSeconds: 60, MaxIntents: 1},
			Script: []ScriptResponse{
				reportFindingScript("SQLi", "Blind SQL Injection (boolean)", "high",
					"不确定：未触发（未验证）", "无法确认：参数表现一致（无可观测差分）。", "http_diff"),
				textScript("无法确认。"),
			},
		},
	}
}

func fingerprintForCase(ef ExpectedFinding) string { return ef.Fingerprint() }
