# Phase VIII.1 — PHASE8_1_SESSION_AUDIT

Session flow: `Login` (browser path: cookie → shared jar) + API path (token → Authorization header pin). `Session.auth_check` — expiry detection.

Unit: `TestLoginCookieJarFlow`, `TestLoginRejectsMissingCreds`, `TestSessionCookieJar`, `TestSessionHeaderInjectedFromContext`, `TestSessionHeaderSkippedWhenKeyEmpty`, `TestSessionHeaderSkippedWhenNoSessionID` — pass.

Live: PowerShell bench-verify — login → 200 + `Set-Cookie: sid=bench-secret`; task 55: `/admin/stats` → 200 (cookie replayed).

Limitation: isolation sequential (process-wide jar); no per-worker identity isolation (documented).
