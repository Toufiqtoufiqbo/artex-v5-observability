package agent

import (
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
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
// clients: environment variables never grant authority, and the logical
// destination passes scopeGuard before dispatch. The ONLY way this transport
// uses a proxy is an explicitly configured recording proxy (see
// SetCaptureProxy), which is part of the trusted platform configuration.
var (
	captureProxyMu sync.RWMutex
	captureProxy   *url.URL
	captureRoots   *x509.CertPool // CA of the recording MITM, when supplied
)

// SetCaptureProxy points target-assessment HTTP clients at the platform's
// recording (MITM) proxy. Passing an empty address disables it. The proxy host
// must be a literal loopback address: a remote proxy would move the dial out of
// the task's pinned-address authorization boundary.
//
// Scope authorization is unchanged: doHTTP calls scopeGuard on the logical URL
// before dispatch, and dialPinnedContext validates the PROXY address too, so no
// destination escapes the task scope simply by routing through this proxy.
func SetCaptureProxy(addr, caCertPath string) error {
	proxy, roots, err := parseCaptureProxy(addr, caCertPath)
	if err != nil {
		return err
	}
	captureProxyMu.Lock()
	captureProxy, captureRoots = proxy, roots
	captureProxyMu.Unlock()
	return nil
}

func parseCaptureProxy(addr, caCertPath string) (*url.URL, *x509.CertPool, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, nil, nil
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, nil, fmt.Errorf("capture proxy: invalid address %q: %w", addr, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, nil, fmt.Errorf("capture proxy: unsupported scheme %q (need http/https)", u.Scheme)
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return nil, nil, fmt.Errorf("capture proxy: missing host")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, nil, fmt.Errorf("capture proxy: host %q is not a loopback IP literal", host)
	}
	if strings.TrimSpace(caCertPath) == "" {
		return u, nil, nil
	}
	pem, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, nil, fmt.Errorf("capture proxy: read CA: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, nil, fmt.Errorf("capture proxy: CA file contains no valid certificate")
	}
	return u, pool, nil
}

func captureProxyConfig() (*url.URL, *x509.CertPool) {
	captureProxyMu.RLock()
	defer captureProxyMu.RUnlock()
	return captureProxy, captureRoots
}

func egressHTTPProxyPolicy(*http.Request) (*url.URL, error) {
	proxy, _ := captureProxyConfig()
	return proxy, nil
}

// captureProxyURL is set on httpCall when the platform explicitly configured a
// recording proxy; doHTTP uses it to authorize the dial address separately
// from the logical target.
func captureProxyURL() *url.URL {
	proxy, _ := captureProxyConfig()
	return proxy
}
