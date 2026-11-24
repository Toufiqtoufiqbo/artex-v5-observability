# PHASE8_4_4_1 — RUNTIME POLICY API (Phase F)

Inspection of policy-modifying surfaces (source, not executed — no code changes made):

| API / Endpoint | Change capability | Authority |
|---|---|---|
| `traffic.SetEgressAllowlist` | Change traffic allowlist | Go API; not exposed over HTTP in this phase |
| `agent.SetTrustedProviderPolicy` | Change trusted provider policy | Go API; not exposed over HTTP |
| `agent.SetProviderEgressPolicy` (VIII.4.4) | Override | Go API (legacy) |
| `enrich.Engine.SetEgressPolicy` | Change enrichment policy | Go API; caller is server-side; not wired (VIII.4.4.1) |
| `selfupdate.SetNetworkEnabled` | Enable self-update | Go API; default false |
| `POST /api/llm` (existing) | Changes BaseURL etc. | Trusted-config surface; the trusted policy (§) is not derived from it |
| `POST /api/tasks` (existing) | Task-level scope | VIII.4.1/2 (target domain) |

Per the phase's requirements:

- **No Worker/model-controlled tool** may modify trusted egress authorization (no new tools added).
- **No ordinary HTTP endpoint** can widen egress (no `/api/egress` etc. introduced).
- Policy updates via the Go APIs are atomic (atomic.Value/atomic.Bool) and are followed by an audit event.
- Empty update to traffic policy (`SetEgressAllowlist(nil, true)`) re-seals → deny-all (fail-closed).
- Sensitive fields: no credentials in API responses or logs (strict scan in `SECRET_AUDIT.md`).

**Posture**: the trusted policies are designed to be deployment-configured and immutable for the process lifetime; the Go APIs are for operators/tests, and wiring to an HTTP config surface is explicitly a follow-up (see `POLICY_INITIALIZATION.md`).

No changes made this phase.
