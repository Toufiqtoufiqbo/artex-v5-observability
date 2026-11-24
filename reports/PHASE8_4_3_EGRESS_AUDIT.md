# PHASE8_4_3 — EGRESS AUDIT (Phase I)

Structured, secret-safe audit records of egress decisions.

## 1. Agent (target assessment) — `agent.SetEgressAuditSink`

`doHTTP` emits (`agent/httpclient.go`):

```go
EgressEventForURL(EgressTargetAssessment, RunInfoFrom(ctx).TaskID, rawURL, EgressDecisionDeny, err.Error())  // on scopeGuard deny
EgressEventForURL(EgressTargetAssessment, RunInfoFrom(ctx).TaskID, rawURL, EgressDecisionAllow, "scope-checked")  // on allow
```

Redirect revalidation emits deny events on failure.

`EgressEvent` fields: `Category`, `Host`, `Port`, `Scheme`, `TaskID`, `Decision` ("allow"/"deny"), `Reason`.

Semantics:
- **Audit ≠ authority.** Events are observational; the sink never grants or denies.
- **No secrets:** no headers, bodies, cookies, or credentials are recorded; only host/port/scheme/reason/task id.
- **No valid-event fabrication:** events are emitted at the decision points (`doHTTP` initial + redirect; traffic hook).
- **Telemetry failure:** if a sink panics, `emitEgressEvent` would panic the caller — bounded by tests (sink is user code; document as sink must not panic — follow-up).

## 2. Traffic proxy — `traffic.SetEgressAuditSink`

Hook in `Open()` emits allow/deny per upstream dial (logical destination host).

## 3. Provider egress (domain B)

- LLM provider client (`norma/agent/provider.go`) — unchanged by this phase; no target-scope reads (grep-verified).
- **To be done (Phase VIII.4.4):** typed provider-domain events (endpoint + port + scheme) at provider construction/call sites.

## 4. Internal services (domain C)

- Health/auth/local services — not instrumented in this phase (explicit configured destinations; verified live: health 200, auth init, DB connectivity).

## 5. Events recorded for this phase (representative)

Via unit tests (`agent/egress_policy.go`; `TestEgressEventsEmitOnAllowAndDeny`):
- allow event for `http://127.0.0.2:3004/ok` (task id), category `target_assessment`, decision `allow`.
- deny event for out-of-scope destination, decision `deny`, reason = scope error string.
- No fields contain secrets (asserted structurally).

## 6. Known gaps

- No cross-domain event correlation yet (single sink per package).
- Enrichment/self-update/notify not instrumented (documented limits).
- Provider domain B instrumentation: follow-up.

## 7. Files

- `agent/egress_policy.go` (new, exported API)
- `agent/egress_policy_test.go` (new, `TestEgressEventsEmitOnAllowAndDeny`)
- `agent/httpclient.go` (audit emissions + proxy policy)
- `traffic/egress.go`, `traffic/traffic.go`, `traffic/egress_test.go` (new/changed)
- `agent/scopecheck.go` — unchanged (VIII.4.1/2 preserved)
