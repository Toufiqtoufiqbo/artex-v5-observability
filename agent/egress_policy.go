package agent

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// VIII.4.3 — unified egress control.
//
// Three distinct authorization domains; they never silently authorize each
// other:
//
//   - EgressTargetAssessment — systems a task may assess (doHTTP / scopeGuard).
//   - EgressLLMProvider      — the configured LLM provider endpoint (norma).
//   - EgressInternalService  — explicitly configured local application services.
//
// The categories are recorded in structured, secret-safe audit events via
// SetEgressAuditSink. The sink is observational only: it never grants or
// denies anything.
// ---------------------------------------------------------------------------

type EgressCategory string

const (
	EgressTargetAssessment EgressCategory = "target_assessment"
	EgressLLMProvider      EgressCategory = "llm_provider"
	EgressInternalService  EgressCategory = "internal_service"
)

// EgressEvent is the structured audit record of one egress decision.
// Deliberately secret-safe: host/port/scheme/reason only — no headers,
// bodies, or credentials.
type EgressEvent struct {
	Category EgressCategory `json:"category"`
	Host     string         `json:"host,omitempty"`
	Port     string         `json:"port,omitempty"`
	Scheme   string         `json:"scheme,omitempty"`
	TaskID   string         `json:"task_id,omitempty"`
	Decision string         `json:"decision"` // "allow" | "deny"
	Reason   string         `json:"reason,omitempty"`
}

var (
	egressAuditMu   sync.RWMutex
	egressAuditFunc func(EgressEvent)
)

// SetEgressAuditSink installs the (optional) audit-event sink. A nil sink
// disables events. The sink must not panic; events are best-effort.
func SetEgressAuditSink(fn func(EgressEvent)) {
	egressAuditMu.Lock()
	defer egressAuditMu.Unlock()
	egressAuditFunc = fn
}

func emitEgressEvent(ev EgressEvent) {
	egressAuditMu.RLock()
	fn := egressAuditFunc
	egressAuditMu.RUnlock()
	if fn != nil {
		fn(ev)
	}
}

// EgressDecisionAllow / EgressDecisionDeny classify audit decisions.
const (
	EgressDecisionAllow = "allow"
	EgressDecisionDeny  = "deny"
)

// EgressEventForURL builds a secret-safe event for a logical destination.
// Used by doHTTP. Scheme/host/port are parsed from the URL; errors surface as
// a deny event for unparseable URLs.
func EgressEventForURL(category EgressCategory, taskID int64, rawURL string, decision string, reason string) EgressEvent {
	ev := EgressEvent{Category: category, Decision: decision, Reason: reason}
	ev.TaskID = egressTaskIDString(taskID)
	if u, err := url.Parse(strings.TrimSpace(rawURL)); err == nil {
		ev.Host, ev.Port, ev.Scheme = u.Hostname(), u.Port(), u.Scheme
	}
	return ev
}

func egressTaskIDString(taskID int64) string {
	if taskID <= 0 {
		return ""
	}
	return strconv.FormatInt(taskID, 10)
}

// egressHTTPProxyPolicy is the VIII.4.3 Phase B policy for target-assessment
// clients: the logical destination passes scopeGuard before dispatch, so the
// transport itself uses no proxy — environment variables alone never grant
// new authorization.
func egressHTTPProxyPolicy(*http.Request) (*url.URL, error) { return nil, nil }
