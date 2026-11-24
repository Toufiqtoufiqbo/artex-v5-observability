# PHASE8_3_4 — PROVIDER PARITY (SECRET-SAFE UPDATE)

**No key, token, or Authorization header is stored in any report or file.**

Configured BOTH versions through authenticated `POST /api/llm` (admin login first; key passed via environment variable only; all printed output redacted):
- `provider=openai`, `base_url=https://integrate.api.nvidia.com/v1`, `model=nvidia/nemotron-3.5-lightning-30b-a3b`, `rate_per_second=0.3`, `rate_per_minute=18`, `context_window_k=128`.
- Baseline :8888 → setLLM **200**, getLLM **200** (configured=true, key_set=true, same model/base_url/provider).
- Modified :8787 → setLLM **200**, getLLM **200** (identical effective fields).

Inference smoke `POST /api/llm/test`: both returned 200 with `ok:false` and the **same** error class (`anthropic: status 401 ... invalid x-api-key`, different request_ids). Root cause: the test endpoint uses a separate side-question/conversation LLM profile (DB profiles `p1`/`conv-pick`, `format=anthropic`, model `claude-x`) — not the main engine profile. This is identical in both versions, not a parity difference.

**Verdict: `SHARED PROVIDER VERIFIED WITH LIMITATIONS`**
1. Same provider: YES. 2. Same exact model: YES. 3. Equivalent settings: YES for main profile. 4. Real inference from both: NOT achieved (main engine not exercised; smoke used a different profile and failed identically). 5. Reach fixture: not tested. 6. Paired pilot: NOT executed (budget). 7. VIII.3.5: not ready until a task-level paired run is performed.

Limitations preserved: baseline archive identity unverified; norma skew v0.4.4/v0.4.3.
