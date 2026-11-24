# PHASE8_2_1 — network boundary + redirects

Network boundary (code inspection): `doHTTP` (`agent/httpclient.go:123-139`) calls `scopeGuard(rawURL)` before client construction/`client.Do`. All new tools route through `doHTTP`. **Counting-transport test with zero-connection proof NOT implemented** — evidence is code-level only.

Redirects (implemented this phase): `agent/httpclient.go:195-219` — when `follow_redirects=true`, `CheckRedirect` revalidates EVERY destination via `scopeGuard`, enforces a 10-hop bound, and strips Authorization/Cookie on cross-origin hops. Default (`follow_redirects=false`) still uses `ErrUseLastResponse`.

Test: `agent/http_redirect_test.go` — `TestRedirectScopeRevalidated`: 302 to `http://localhost:1/out` (host localhost, scope ip=127.0.0.1) → rejected with scope error; 302 relative `/out` → allowed, status 200.

`go test ./agent/ -count=1` → `ok ... 0.322s`.

Limitations: DNS/actual connection destination, retries, IPv4/IPv6, concurrent scope checks, credentials-forwarding test with two local hosts — NOT verified.
