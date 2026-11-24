# PHASE8_3_4 — PROVIDER INVENTORY + FINAL

Environment (verified): baseline 8888 pid 11080; modified 8787 pid 12736; fixture 3004 pid 1120. Both healthy (prior phase), separate DBs/ports/logs.

Source inventory: both trees expose the same LLM configuration surface — `server/dto.go:592,627` (LLM profile DTOs / API), `server/manager.go:579` (baseline) vs `server/manager.go:564` (modified). Both use the `norma` provider abstraction with OpenAI-compatible custom base URL / model / API-key fields. No source patch needed in principle.

| Capability | Baseline | Modified v5 |
|---|---|---|
| OpenAI-compatible API | supported via profile config | supported via profile config |
| Custom base URL / model | supported | supported |
| API key from protected config | supported | supported |
| Temperature / max tokens / timeout | profile fields | profile fields |
| Planner/worker model selection | manager config | manager config |

Limitation preserved: baseline archive identity unverified; `norma v0.4.4` vs v0.4.3.

**Verdict: `PARITY BLOCKED`**

Answers: (1) same provider is *architecturally* supported by both, but (2) exact-model parity was not established, (3) inference settings not applied to baseline, (4) neither performed a real inference request in this phase, (5) no agent reached the fixture, (6) no paired pilot was executed, (7) Phase VIII.3.5 is NOT ready.

Blocker: credentials are not available in this session and must never be printed or committed; baseline starts with no provider configured. Outbound egress is restricted to the approved endpoint + local lab; no mock provider was substituted. Per stop conditions, stop without requesting credentials.

Reports not produced: `PHASE8_3_4_PROVIDER_PARITY.md`, `PHASE8_3_4_INFERENCE_SMOKE.md`, `PHASE8_3_4_EXECUTION_TRACE.md`, `PHASE8_3_4_PILOT_RESULTS.md` (no observations to report).
