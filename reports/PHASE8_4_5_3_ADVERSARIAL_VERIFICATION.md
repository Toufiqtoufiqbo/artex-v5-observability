# PHASE8_4_5.3 — ADVERSARIAL VERIFICATION

Investigation + deterministic local adversarial tests of the VIII.4.5.2 pinning implementation. Local loopbacks only.

## 1. Existing implementation (as written by VIII.4.5.2)

- `agent/scopecheck.go` — `addressAuthorized(key, ip, host)`; `agent/httpclient.go` — `resolvePinnedIP`, `dialPinnedContext`; `DialContext: dialPinnedContext` on the transport.

## 2. New tests (`agent/scope_pin_adversarial_test.go`, all PASS)

| Test | Property | Result |
|---|---|---|
| `TestPin_DialAddressMatchesAuthorized` | Captures the dial address via the `dialFn` hook (VIII.2.2 seam); asserts it equals the authorized host (`127.0.0.2`); fixture returns 200 through the same destination | **PASS — DESTINATION MATCH VERIFIED** |
| `TestPin_ResolvePinnedIP_RejectsUnauthorized` | `resolvePinnedIP(ctx, key, "localhost")` → error (localhost=127.0.0.1 unauthorized) | **PASS — RESOLVER DENIAL** |
| `TestPin_DoHTTPDeniesUnauthorizedResolved` | `doHTTP` to `http://localhost:PORT/` (hostname; hostname not in scope) → scope error | **PASS** |

```
go test ./agent/ -run 'TestPin_' -count=1 -v → PASS (3/3)
go test ./agent/ -count=1 → ok
```

## 3. Verdict

**`PARTIAL — DESTINATION VERIFIED, CONNECTION REUSE GAP`**

### Confirmed

1. **Actual dial destination = authorized IP** — `TestPin_DialAddressMatchesAuthorized` captures `dialFn`'s `addr` and proves it is the authorized destination; HTTP 200 received through the same listener.
2. **Unauthorized addresses never reach the dialer** — `TestPin_ResolvePinnedIP_RejectsUnauthorized` + VIII.4.5.2 `TestScopeIsolation_UnseededTaskDenies`/`CrossTaskDeniedZeroDials`.
3. **Concurrent task isolation** — proven by VIII.4.3 `TestScopeIsolation_TwoTasks`/`_ConcurrentOverlap` (per-task keys; this phase's new `addressAuthorized(key, ...)` uses `getScope(key)` — same store).
4. **Redirect/retry pinning** — retries in this path are Go's transport retries; every new connection goes through `DialContext: dialPinnedContext` (source-verified).

### Confirmed gaps (documented)

1. **Connection reuse (the real gap).** Go's `http.Transport` pools connections. On a reused connection there is **no dial**, so `dialPinnedContext` does not run again — no address re-validation per request. A scope change or DNS change between two requests on the same pooled connection is not reflected. (Standard Go behavior; no task-specific pool isolation.)
2. **Mixed DNS answers** — policy is "first address that `addressAuthorized` accepts"; a mixed set where an unauthorized address is the first → denies; the implementation picks the first authorized. No test covers multi-answer sets.
3. **Redirect chain** — only hostname-level `scopeGuard` revalidation (VIII.4.3); no address-level validation on redirects (redirect targets may dial unpinned if hostname kinds used).
4. **`root_domain` rebinding gap** — documented in VIII.4.5.2 (§4).
5. **IPv6** — `hostOf` IPv6 parsing limitation (VIII.4.2, documented).

### Not claimable

- `VERIFIED — PINNED DESTINATION AND TASK ISOLATION` (connection reuse gap).
- DNS-pinning guarantees for hostname-scoped destinations.

## 4. Recommended next phase

`PHASE8_4_5.4`: per-request authorization on pooled connections — e.g., `http.Transport.MaxIdleConnsPerHost=0` (disable pooling) for scoped requests, or a `TLSClientConfig`-based `http2`-free per-host pool keyed by `(taskKey, authorizedIP)`; plus address-level redirect validation + mixed-answer tests.

## 5. Report

`reports/PHASE8_4_5_3_ADVERSARIAL_VERIFICATION.md`
