package agent

import (
	"context"
	"encoding/json"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
)

// ---------------------------------------------------------------------------
// Strategy — the methodology that pairs with the oracles.
//
// An oracle proves a fact about one parameter. The strategy says how an
// engagement composes: where to start, when to use which tool, and how findings
// chain into higher-value ones. It is injected into the worker prompt and also
// served as a callable tool for the agent to re-read.
// ---------------------------------------------------------------------------

const strategyText = `
ASSESSMENT STRATEGY (supplemental)

Phase 1 — RECON (use Crawl). Start at the entry point. Use every discovered
endpoint (links, forms, js_paths) — do not invent "/api/report" style guesses,
that is what this tool was built to eliminate.

Phase 2 — AUTHENTICATION. Authenticate once with Login (browser-style login
returns a cookie in the jar; API-style login returns a token that gets pinned
as an Authorization header). Verify the identity with one HTTPRequest call and
confirm you are really authenticated.

Phase 3 — AUTHZ / IDOR. With an authenticated session, test cross-object
access: same endpoint, change the object id by one step (e.g. request
/api/Users/25 after /api/Users/26). Compare status and response body — a
cross-user read or a write that succeeds without touching /profiles is an
IDOR finding. Report the pair of requests as evidence.

Phase 4 — VULNS (use Probe). For every suspicious parameter run the matching
oracle: boolean/time for injection, reflection for XSS, eval for template
injection, path for traversal. Trust only CONFIRMED verdicts, and cite the
measured numbers in the evidence.

Phase 5 — CHAINING. When one finding creates an identity (session, token,
crashed authz), it converts every other finding on the same host into impact:
the escalation of one role reveals everything. File separate findings per
primitive; the chain is the story for impact.

Phase 6 — RETEST. After a finding is confirmed, note the exact reproduction
(the oracle output). Repeat it after fixes and report the result with the
new measurement; only keep the finding if it still confirms.

Phase 7 — HUMILITY. Not-confirmed is an outcome, not a defeat: record a fact
("param appears injectable — not confirmed by oracle") instead of a finding.
Confidence goes to the evidence, not to the intuitions.
`

func newStrategyTool() actool.CoreTool {
	schema := obj(map[string]any{
		"action": map[string]any{
			"type":        "string",
			"enum":        []string{"list", "reset"},
			"description": "list: return the strategy text. reset: none (kept in mind).",
		},
	})
	desc := "Return the supplemental assessment methodology for this engagement: recon, authentication, authz/IDOR, oracles, chaining and retest. Call it when you are unsure which tool to use next or want a fresh look at the methodology."
	return writeTool("Strategy", desc, schema, func(_ context.Context, in json.RawMessage) (actool.Result, error) {
		if strings.Contains(string(in), `"reset"`) {
			return actool.Text("strategy acknowledged — use Phase 1 as your next step"), nil
		}
		return actool.Text(strategyText), nil
	})
}
