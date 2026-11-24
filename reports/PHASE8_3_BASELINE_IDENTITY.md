# Phase VIII.3 — BASELINE IDENTITY

**Published baseline archive:** `artex-global-main.zip`
- SHA-256: `93AFBEC089A9D8AB00AE1DA4918DFCAF3A99AF636C1303DAE514F4EBE7CD11E2`
- Size: 8,944,269 bytes
- Extracted root: `artex-global-main/` (verified)

**Reference commit** `133f3cf97f2d153227a1a95d3c4b7cd5b72020a6` (jiwoochris/artex-global): **NOT verifiable locally** — the extracted folder is not a git repository (`fatal: not a git repository`). Commit identity remains unverified in this environment.

**Modified tree:** `artex-original` @ e6ec569 (0.3.15) + Phase VIII/VIII.1/VIII.2/VIII.2.1/VIII.2.2 changes.

**File-level diff (executed):** base files=794, modified files=60,343 → ADDED=59,746, REMOVED=197, CHANGED=574. The diff is dominated by build artifacts (`server/webui/dist/_next/**` — 59k+ files) present only in the modified tree; the archive ships no `dist`. Source-level additions (this work): `agent/finding_gate.go`, `agent/httpclient.go`, `agent/scopecheck.go`, `agent/strategy.go`, `agent/tools_http.go`, `agent/tools_probe.go`, `agent/tools_login.go`, `agent/tools_attackpath.go`, `agent/tools_retest.go`, `agent/tools_diff.go` + their tests. REMOVED source files include `agent/outputlang.go` and `.github/` templates (archive vs tree difference — needs reconciliation; not verified as intentional). CHANGED includes `agent/assembly.go`, `agent/capture.go`, `agent/chat.go`, `agent/compaction.go`, `agent/deferred.go`, `agent/finding_recorder.go` — these are **upstream v0.3.15 vs artex-global differences**, not this mission's edits; reconciling them requires the baseline build (pending).

Limitation: no git metadata in the archive; commit identity cannot be confirmed without network access (not used).
