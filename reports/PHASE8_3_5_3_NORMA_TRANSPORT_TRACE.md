# PHASE8_3_5_3 — NORMA TRANSPORT TRACE (budget-limited)

**Verdict: `PARTIAL — BUDGET EXHAUSTED`**

## Scope and identities
- Baseline: `artex-global-main.zip` SHA-256 `93AFBEC0…CD11E2`; extracted `%TEMP%\opencode\artex-base\artex-global-main`; no git metadata (commit UNPROVEN).
- Modified v5: `artex-original` @ `e6ec569`, dirty tree documented in `PHASE8_3_1_MODIFIED_SNAPSHOT.md`.

## Dependency resolution (evidence labels)
- `SOURCE VERIFIED`: both `go.mod` declare module `github.com/Autumn-27/norma`; modified requires **v0.4.3**, baseline requires **v0.4.4** (verified in Phase VIII.3.1; not re-verified this phase).
- `DEPENDENCY VERIFIED` (from prior local inspection): module cache contains both `norma@v0.4.3` and `norma@v0.4.4` under `C:\Users\Rahim\go\pkg\mod\github.com\!autumn-27\`; the `tool/` package (bash.go, webfetch.go, …) was inspected earlier. Concrete **LLM provider** package files were **NOT** located this phase → `UNKNOWN`.

## Not established this phase (all `UNKNOWN`)
Concrete HTTP client/transport for provider calls; dispatch function; response receipt/parse/return points; engine-vs-Worker provider construction differences; retry/fallback implementation; safe instrumentation points. No source was modified; no live inference; no dependency changes.

## Minimum instrumentation
Cannot be specified without the transport trace → `UNKNOWN`.

## Next step
Read `norma@v0.4.3` and `@v0.4.4` provider packages (likely `llm`/`provider` dirs) and `server/llmpool.go`; locate the RoundTripper handoff and parse-return; then map both versions.

No claims about live inference, parity, or superiority. Previous reports preserved.
