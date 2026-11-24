# PHASE8_4_3 — IMPLEMENTATION (Phase F)

## 1. Design — three authorization domains

`agent/egress_policy.go` (new):

```go
type EgressCategory string
const (
    EgressTargetAssessment EgressCategory = "target_assessment"  // doHTTP / scopeGuard (VIII.4.1)
    EgressLLMProvider      EgressCategory = "llm_provider"       // norma LLM client (domain B)
    EgressInternalService  EgressCategory = "internal_service"   // domain C
)

type EgressEvent struct {
    Category EgressCategory
    Host, Port, Scheme string
    TaskID   string
    Decision string // "allow" | "deny"
    Reason   string
}
```

- Typed categories instead of one ambiguous boolean; categories never authorize each other.
- `SetEgressAuditSink(fn func(EgressEvent))` — observational only. Events are secret-safe (host/port/scheme/reason/task id only; no headers/bodies/cookies).

## 2. Egress audit events (Phase F + Phase I)

`doHTTP` (`agent/httpclient.go`):

```go
if err := scopeGuard(ctx, rawURL); err != nil {
    emitEgressEvent(EgressEventForURL(EgressTargetAssessment, int64(RunInfoFrom(ctx).TaskID), rawURL, EgressDecisionDeny, err.Error()))
    return nil, err
}
emitEgressEvent(EgressEventForURL(EgressTargetAssessment, int64(RunInfoFrom(ctx).TaskID), rawURL, EgressDecisionAllow, "scope-checked"))
```

Redirect revalidation emits a deny event on failure.

Traffic proxy (`traffic/traffic.go` + `traffic/egress.go`): upstream hook emits allow/deny events via `SetEgressAuditSink`.

**Audit semantics:** a telemetry failure must not grant authorization — events are emitted after/around decisions; nothing in the audit path mutates state.

## 3. Explicit destination classification

- Target: `EgressEventForURL` parses scheme/host/port from the logical URL (`url.Parse`).
- Proxy: the traffic proxy records the **logical** destination (`egressDestHost(req)` — `req.Host`/`req.URL.Host`), not the proxy socket address. Distinguishing logical vs proxy connection is a documented follow-up.

## 4. Phase B — proxy policy for target clients

`agent/egress_policy.go`:

```go
func egressHTTPProxyPolicy(*http.Request) (*url.URL, error) { return nil, nil }
```

Used as `http.Transport.Proxy` in `doHTTP`: environment proxies (`HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`) do **not** silently authorize target destinations. Logical targets pass `scopeGuard` before dispatch; clients go direct. (Explicit per-request proxy allowlists are documented follow-ups.)

## 5. Minimal changes

- `agent/egress_policy.go` (new)
- `agent/httpclient.go` (audit emissions + proxy policy)
- `traffic/egress.go`, `traffic/traffic.go`, `traffic/egress_test.go` (new/changed)
- `agent/scopecheck.go` — unchanged (VIII.4.1/2 preserved)

## 6. Internal service access (domain C)

- Local application services (health, auth, DB) are explicitly configured destinations — no change this phase; verified at runtime (health 200, auth init, DB connectivity in `PHASE8_4_3_RUNTIME_VALIDATION.md`).

## 7. Follow-ups (not this phase)

- Explicit target-assessment proxy allowlist (typed).
- LLM provider destination validation (typed category B, base URL + port + scheme checks).
- Enrichment destination policy.
- DNS-to-dial pinning (documented separately in VIII.4.4 plans).
