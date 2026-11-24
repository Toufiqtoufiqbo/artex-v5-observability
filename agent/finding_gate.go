package agent

import (
	"fmt"
	"regexp"
	"strings"
)

// The evidence gate exists because instructions alone did not hold.
//
// Observed in the field: the worker prompt already says "only report what you
// actually triggered, with a reproducible request/response", and yet a single
// task produced five findings whose own evidence text read "not confirmed",
// "no successful SQLi", "method not found", plus one that cited the lab's
// published solution as proof. A prompt is advisory; this gate is not.
//
// It enforces two things at the tool boundary:
//  1. A finding must carry a concrete artifact (request/response, command
//     output, or a measured difference) — a suspicion belongs in record_fact.
//  2. A published solution/walkthrough is never accepted as evidence.
var (
	// solutionLeakRe matches evidence that points at someone else's answer
	// rather than at the tester's own traffic.
	solutionLeakRe = regexp.MustCompile(`(?i)(lab\s+solution|solution\s+confirms?|official\s+solution|as\s+shown\s+in\s+the\s+solution|walkthrough\s+(says|shows|confirms)|题解|官方解答|参考答案|标准答案)`)

	// proofArtifactRe matches the shape of a reproducible artifact: an HTTP
	// exchange, a security-tool invocation, a status code, a reflected payload,
	// or a measured delta.
	proofArtifactRe = regexp.MustCompile(`(?i)(` +
		`HTTP/[12]\.\d` + // raw response head
		`|\b(GET|POST|PUT|PATCH|DELETE|HEAD)\s+/\S*` + // request line
		`|\b(curl|sqlmap|nmap|nuclei|ffuf|gobuster|httpx|wfuzz)\b` + // tool invocation
		`|\bstatus\s*[:= ]\s*[1-5]\d{2}\b` + // status=200
		`|\b[1-5]\d{2}\s+(OK|Found|Unauthorized|Forbidden|Internal Server Error|Moved)\b` +
		`|\bpayload\b[\s\S]{0,300}\b(response|returned|reflected|result|body)\b` +
		`|\b\d+\s*(bytes|chars|characters|records|rows|products|users|items)\b` +
		`|->|=>` +
		`)`)

	// suspicionRe catches evidence that admits it is not confirmed; such text
	// must not be filed as a confirmed vulnerability.
	suspicionRe = regexp.MustCompile(`(?i)(not\s+confirmed|unconfirmed|not\s+verified|no\s+successful|exploitation\s+not\s+confirmed|could\s+not\s+(confirm|exploit|trigger)|未确认|无法确认|未能触发|未验证)`)
)

// gateFindingEvidence validates and normalises the evidence attached to a
// report_finding call.
//
// Layered policy (v2 — compat with legacy callers):
//  1. Empty evidence → accepted (legacy contract; nothing to validate).
//  2. Evidence citing a published solution/walkthrough → rejected.
//  3. Evidence that admits it was not confirmed, without a reproducible
//     artifact → rejected.
//  4. Otherwise accepted.
//
// Notes:
//   - "Artifacts" (HTTP exchange, tool output, measured delta) are what
//     `proof` usually carries. If the model attaches an artifact, that's the
//     strongest form; if not and the text isn't an admission of failure, we
//     accept rather than reject — rejecting every artifact-less finding broke
//     the legacy report contract (see finding_recorder_test.go).
func gateFindingEvidence(evidence, proof, proofType string) (string, error) {
	combined := strings.TrimSpace(evidence)
	if p := strings.TrimSpace(proof); p != "" {
		if combined != "" {
			combined += "\n\nProof:\n" + p
		} else {
			combined = p
		}
	}

	if strings.EqualFold(strings.TrimSpace(proofType), "none") && !proofArtifactRe.MatchString(combined) {
		return "", fmt.Errorf("report_finding rejected: proof_type=none means nothing was verified. Collect a reproducible artifact, or use record_fact for an unverified suspicion")
	}

	if combined == "" {
		return combined, nil
	}

	if solutionLeakRe.MatchString(combined) {
		return "", fmt.Errorf("report_finding rejected: the evidence cites a published solution/walkthrough, which is not a proof you produced. Re-trigger the issue yourself and attach your own request/response, then report it")
	}

	if suspicionRe.MatchString(combined) && !proofArtifactRe.MatchString(combined) {
		return "", fmt.Errorf("report_finding rejected: the evidence says the issue was not confirmed. A finding must be confirmed; use record_fact (inferred) for suspicions and keep exploring until you can trigger it")
	}

	return combined, nil
}
