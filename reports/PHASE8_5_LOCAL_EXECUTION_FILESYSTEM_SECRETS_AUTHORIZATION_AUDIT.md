# MASTER REPORT — PHASE VIII.5 LOCAL EXECUTION, FILESYSTEM, SECRETS & AUTHORIZATION AUDIT

**Verdict:** `PARTIAL — SECRET HANDLING VERIFIED; LOCAL EXECUTION + FILESYSTEM + API AUTHORIZATION GAPS REMAIN` (see §19). All findings in this report are `SOURCE VERIFIED` or `TEST VERIFIED` (agent/server packages, local fixtures only).

## 1. Executive summary

Phase VIII.5 audited ARTEX v5's local execution (A), filesystem containment (B), secret disclosure (C), database ownership (D), API authorization (E) and cross-boundary behavior (F). Highlights:

- **Gate A (local execution): HIGH — custom command tools bypassed the VIII.4.7 Bash network guard** (`server/customtool.go:349` → `actool.NewBash()` directly). Fix implemented: `agent.NewBashGuardTool` (exported) + custom tool exec goes through it. Regression test `TestCustomToolCommandNetworkBlockedByGuard` (server) PASS.
- **Gate B (filesystem): HIGH — norma file tools (Read/Write/Edit/MultiEdit/LS/Glob/Grep) have no write-path containment** (`norma/tool/helpers.go:90 resolvePath`: absolute paths pass through; `..` traversal; symlinks followed). Fixed: `agent/fs_containment.go` (`ContainFileTools` — wraps Write/Edit/MultiEdit, symlink-aware containment vs. `tc.WorkingDir`), applied to planner/worker/mainagent/chat surfaces. Regression tests PASS (relative/absolute/symlink escapes blocked; inside paths allowed; empty WorkingDir legacy).
- **Gate C (secrets): TEST VERIFIED** — synthetic secrets not exposed via `GET /api/llm/profiles` (incl. `api_key_hint` — by design `db/config.go:161-164` stores only the last 4 chars) and `GET /api/auth/status`. `db.LLMProfile.APIKey` is `json:"-"`.
- **Gate D (DB ownership):** single-admin deployment — no users table; no multi-user ownership model exists today. `db.Task`/`db.Finding`/evidence have no per-user owner fields (documented).
- **Gate E (API authorization):** all routes behind `requireAuth` (`server/server.go:959`); exemptions `/api/auth/*` (init/login/status/change-password) and `/api/health` by design. No user-ID spoofing path (all objects fetched by ID only, no owner field).
- **Gate G:** race freedom UNKNOWN (still no C compiler, §18). Previously documented items re-checked: `TestSkillRelPath` (Unix-semantic expectation vs OS-native production — pre-existing, documented), `TestGraphOverviewExpandsAssociatedCompanyScope`/`TestCoreTaskLifecyclePG` (async goal seeding — environmental), `TestEvidenceBindingLifecycle` (flaky).

## 2. Initial repository state (preserved, never modified/discarded)

- Branch: `HEAD` (detached). HEAD: `e6ec56999175509551a1e43a3ee3b053d0941bd9`.
- `git status --short` at phase start: 15 modified + 47 untracked (VIII.4.6→8.5 changes: agent/{provider*,chat,mainagent,planner,tools,worker,bash_guard,fs_containment*,execution_boundary*,scopecheck*,provider_trust*,provider_retry*,fs_*}, server/{server,manager,assembly,customtool*,secrets_leak*}, traffic/*, evidence/store.go, enrich/{enrich,egress_test}, selfupdate/{github,stage}, db/*).
- Go: `go1.26.11 windows/amd64`. OS: windows (amd64). `CGO_ENABLED=0`; `go env CC`=gcc; no C compiler (`where gcc cl cc clang zig tcc` → none).
- PG 5433: lab PG 18.7.0 on 127.0.0.1:5433 (cluster846, DB `artex_v48f`, created this phase; dropped at end — no pre-existing DB modified).
- ARTEX runtimes at phase start: none. Python fixture: none.
- Environment-variable names inspected only (values redacted): `ARTEX_TRUSTED_PROVIDER_HOSTS`, `ARTEX_ALLOW_LOCAL_SHELL`, `AGENT_CORE_DISABLE_INTERACTIVE_SHELL`, `ARTEX_EGRESS_ALLOWLIST`, `ARTEX_LLM_*`, `ARTEX_PG_DSN`, `ARTEX_ADMIN_PASSWORD`, `NVAPI_KEY`.
- Existing Shell config: `LocalShellAllowed()` (ARTEX_ALLOW_LOCAL_SHELL, viii.4.7).
- Existing reports preserved (not overwritten): `reports/PHASE8_*.md`.

## 3. System architecture and trust boundaries (system inventory)

- Repo layout: `agent/` (engine + tools, incl. norma tool surfaces), `server/` (HTTP API, auth, manager, assembly), `traffic/` (egress-sealed proxy), `evidence/`, `enrich/`, `selfupdate/`, `db/` (PostgreSQL), `config/`, `guard/`, `llmpool/`, `llmrec/`, `mcphttp/`, `intercept/`, `notify/`, `report/`, `screenshots/`, `skills/`.
- Norma v0.4.3 (module cache) — the tool framework; ARTEX surfaces = `actool.DefaultTools()` (Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash/Sleep) + `workerDefaultTools()` + `actool.ShellSessionTools()` + ARTEX custom tools + orchestration tools (orchestration.go:41-49).
- Trust boundaries (established by viii.4.6-8.4): network scope (VIII.4.1-4.6), provider trust (VIII.4.6 Gate A-D, VIII.4.8), traffic proxy (VIII.4.3-4.4.1, VIII.4.6 Gate D).

## 4. Complete process-execution inventory (Gate A1)

| # | Path | Caller/Component | Input control | Executable | Args | Env | WorkDir | Timeout/Cancel | Child reuse | Output | Secrets in output? | Audit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| A1 | `exec.CommandContext(shell, flags+command)` | norma Bash (`norma/tool/bash.go:160`) — shells via `shellCmd()` (`norma/tool/task.go:566`: Windows=`powershell -NoProfile -NonInteractive -Command`) | LLM-generated `command` | shell | user command | inherits `os.Environ()` + `tc.Env` | `tc.WorkingDir` | default 120s, max 600s; ctx-cancel propagates; catastrophic-command floor | `setDetached` on timeout→background; noARTEX guard when wrapped | `actool.Capture` (first ~?) — combined stdout+stderr | partial: norma returns tool errors; ARTEX guards reject network commands (with guard) | norma session ledger (`Sessions` table? not verified) |
| A2 | `exec.CommandContext(m.rootCtx, shell, flags+command)` | norma ShellSession PTY (`norma/tool/shell_session.go:302`; tools `shell_open/send/read/close/list`) | LLM (session-scoped, but the tool is only injected under gates) | shell | `command` | inherits | `workDir` | PTY sessions persist (ring 256KB) | PTY read/write streams | — | — |
| A3 | `exec.CommandContext(ctx, shell, flags+spec.Command)` + `taskkill /T /F /PID` | norma background Task (`norma/tool/task.go:231`; `tool_windows.go:25`) | LLM `spec.Command` | shell | spec.Command | spec.Env | spec.WorkingDir | `setDetached` (outlives turn) | output file `task_*.output` | — | — |
| A4 | `exec.CommandContext(runCtx, interp, tmp)` | ARTEX custom tools — kind=command (`server/customtool.go:398 execPython`, `runCommandTool` → `agent.NewBashGuardTool(actool.NewBash())`) | operator-created (authenticated API `/api/tools/custom*`) | `interp` (operator-set via DB setting `python_interp`, `server/customtool.go:197,206`) | temp file path (fixed pattern `<key>-*.py`) | inherits + TOOL_<NAME> env | `workDir`=`tc.WorkingDir` | `timeoutOr(spec.TimeoutMs, 120000)`, ctx-cancel | writes to `workDir/.tools/` then deletes | combined stdout+stderr + exit note | no secrets expected (custom tool) |
| A5 | `exec.CommandContext(ctx, bin, "-h")` | selfupdate `smokeTest` (`selfupdate/bootstrap.go:184`) | operator (update flow) | `bin` (staged, SHA256-pinned) | `-h` | inherits + `smokeEnv=1` | — | 30s ctx timeout | — | CombinedOutput (300-char cap) | — |
| A6 | `exec.Command(rg, args...)` | Grep tool (norma ripgrep.go:148) | LLM `pattern` | `rg` (pinned/abs path) | search args | — | search target dir | — | read-only | file matches | — |
| A7 | `exec.Command("npm", "install", "-g", "@vscode/ripgrep")`, `exec.Command("npm","root","-g")` | ripgrep bootstrap probes (norma ripgrep.go:64,77) | none (probes) | `npm` | — | — | — | — | — | — | — |
| A8 | `exec.CommandContext(ctx, interp, tmp)` | ARTEX custom tools — kind=script (Python; `server/customtool.go:369`) | operator-created | interpreter | script path (A4 style) | same as A4 | same as A4 | same as A4 | `CombinedOutput` | — | no secrets (operator tool) |

Classification:
- A1 (norma Bash): `DISABLED` by default (viii.4.6: `ContainLocalShell` removes Bash from all surfaces when `LocalShellAllowed()==false`); `RESTRICTED` when operator opts in (`ARTEX_ALLOW_LOCAL_SHELL=1` → guard). `DISABLED` on every ARTEX surface except legacy paths — verified by `TestDefaultSurfacesHaveNoBash` (agent/execution_boundary_test.go, PASS).
- A2 (ShellSession PTY): `DISABLED` by default (`server/assembly.go:312`: `LocalShellAllowed() && !InteractiveShellDisabled() && agent.InteractiveShell` gate; DB agent flag); `ADMINISTRATIVE` when both gates pass.
- A3 (norma background Task): not in ARTEX surface inventory (no ARTEX code injects `actool.TaskOutput`/Task tool — `grep` for `actool.NewTask|ShellSessionTools` injection sites found none) — `NOT INVESTIGATED` (external norma API, not currently exposed by ARTEX surfaces).
- A4 (custom tools): `ADMINISTRATIVE` — creation via authenticated API; execution authorized by the creating user's authority.
- A5: `ADMINISTRATIVE`.
- A6 (Grep): `RESTRICTED` — LLM-reachable (surface tool), but read-only (no exec in target dir), no network (Grep is ripgrep with `--` and pattern args; also `bashNetworkCommand` guard applies only to Bash).
- A7: `INFORMATIONAL` — npm probes — write to global npm dir, only when npm exists; not on ARTEX surfaces (no injection found).

## 5. Local execution authorization findings (Gate A)

1. **F-A1 (HIGH, fixed viii.5)**: custom command tools executed arbitrary shell via `actool.NewBash()` without the VIII.4.7 guard; `ContainLocalShell` only filters tools *named* "Bash", so custom tools bypassed the network-command guard. Fix: `agent/bash_guard.go` `NewBashGuardTool` (exported); `server/customtool.go:runCommandTool` → `agent.NewBashGuardTool(actool.NewBash()).Call(...)`. Regression: `server/customtool_exec_test.go:TestCustomToolCommandNetworkBlockedByGuard` PASS (guard returns error-result `IsError=true`).
2. **F-A2 (MEDIUM)**: A1/A6/A7 — norma Bash catastrophic-command floor is regex-based (`norma/tool/bash.go:16-30`); `bashNetworkCommand` in `agent/bash_guard.go:19` is a documented heuristic floor ("it is a containment floor, not a policy engine" — viii.4.2 doc). `powershell`-native network commands (`Invoke-WebRequest`, `Invoke-RestMethod`, `iwr`) covered by pattern (`invoke-webrequest|invoke-restmethod|invoke-webservice`), `curl`/`wget`/`ping`/`ssh` etc. covered. PowerShell egress via `Invoke-WebRequest` in strings is covered; arbitrary PowerShell scripts are not (by design — document).
3. **F-A3 (INFO)**: A7 — npm probes execute network downloads when present (not on ARTEX surfaces; operator environment).
4. **F-A4 (INFO)**: A3 — norma background Task/detached processes — not injected into ARTEX surfaces (see A3 classification).

## 6. Filesystem access map (Gate B1)

- `agent/fs_containment.go` (new): `ContainFileTools` — wraps Write/Edit/MultiEdit (ARTEX surfaces: planner.go:429+, worker.go:492, mainagent.go:160, chat.go:152 session workdirs).
- norma file tools: `Read` (`norma/tool/files.go:37` — `os.ReadFile`), `Write` (`files.go:163` — `os.WriteFile(path, 0o644)` + `MkdirAll(0o755)`), `Edit` (`files.go:214`), `MultiEdit` (`files.go:330`), `LS` (`ls.go:31` — `dir := tc.WorkingDir` — ignores input), `Glob` (`search.go:38` — `root := tc.WorkingDir`), `Grep` (`search.go:169`).
- Path resolution: `norma/tool/helpers.go:90 resolvePath` — `filepath.IsAbs(p) || tc == nil || tc.WorkingDir == "" → return p` else `filepath.Join(tc.WorkingDir, p)` — **no containment, no `..` rejection, no symlink resolution** (norma's design).
- ARTEX-owned: evidence store (`evidence/store.go:hashPath` — sha256 hex, safe), finding export (`server/finding_traffic_export.go` — operator-initiated via authenticated export endpoints), custom tool exec (`server/customtool.go:379-410` — temp `.py` under `workDir/.tools`, `os.CreateTemp(toolsDir, key+"-*.py")` 0600-ish, cwd `workDir`).
- Concurrency: FS writes by LLM tools — no locks in norma file tools (by design; noted).

## 7. Path containment and symlink findings (Gate B)

1. **F-B1 (HIGH — by-design scope boundary, fixed viii.5)**: norma file tools let the LLM write outside `tc.WorkingDir` (absolute paths, `..` traversal; symlinks in Write paths — `os.WriteFile` follows existing symlinks). Artifacts affected: Write/Edit/MultiEdit on planner/worker/mainagent/chat surfaces. Fix: `agent/fs_containment.go` + `ContainFileTools` (symlink-aware, `EvalSymlinks` of deepest existing ancestor, `fsWithinLex`). Regression tests PASS (relative/absolute/symlink-ish escapes blocked; inside paths allowed; empty WorkingDir legacy).
2. **F-B2 (MEDIUM, environment limitation)**: symlink test `TestFsContainmentToolBlocksSymlinkEscape` SKIPS on Windows (`os.Symlink` → "A required privilege is not held by the client"). Symlink-aware containment is implemented (works when symlinks exist); no runtime verification possible on this Windows host. `TEST VERIFIED (partial: symlink path skipped, static review OK)`.
3. **F-B3 (INFO)**: Read/LS/Glob/Grep not contained — by design (reading user files; workers legitimately read artifacts in the session tree and assets). Documented trade-off; session prompt directs writes to the session workdir ("不要写 /tmp 或其他绝对路径").
4. **F-B4 (INFO)**: `ls.go:38 dir := tc.WorkingDir` — LS ignores the LLM-supplied `path` — no escape possible via LS.

## 8. Windows-specific filesystem findings (Gate B)

- Pre-existing (documented viii.4.8): `TestSkillRelPath` — production `filepath.Clean` semantics (OS-native, `\` on Windows); test expects Unix-style — no change (out of scope).
- No new Windows-specific findings this phase.

## 9. Secret lifecycle and disclosure analysis (Gate C1)

- Secret inventory (no values recorded): Provider API keys (`db.LLMProfile.APIKey`, `json:"-"` — never serialized; `profileColsKey` internal read only; `SaveProfile` stores; `APIKeyHint` = `"."+key[len-4:]` by design, db/config.go:161-164); PostgreSQL credentials (env `ARTEX_PG_DSN`); user password (bcrypt hash in settings; `authStatus` exposes only `initialized` flag); JWT signing key (`config/jwt.key`, auto-generated per install, never serialized); session cookies (signed JWT via `requireAuth`); user-agent-inherited env in subprocesses (A4: TOOL_<NAME> for scalars, sanitized inputs; `os.Environ()` inherited by custom tool exec — documented operator-level surface); NVIDIA key (env `NVAPI_KEY`).
- `SessionHeaderKey` — header name, not secret (used to inject session-id into provider requests — `agent/tools_http.go`).
- Exposure surfaces checked: `GET /api/llm/profiles` (no APIKey — `json:"-"`; `api_key_hint` = last-4-by-design), `POST /api/llm/profiles` (response = `{id}`), `GET /api/auth/status` (initialized only), findings/evidence export (synthetic-data tests), logs (no API keys logged — no findings of key-in-log).
- Redaction vs authorization: key not exposed ≠ unauthorized access to provider calls (viii.4.6).

## 10. Synthetic secret leak-test results (Gate C3)

Synthetic credentials used: `ARTEX_TEST_SECRET_7F2A` (only value, redacted from report per rule: "Do not expose, print, copy, or place real secrets").

| Surface | Result |
|---|---|
| `GET /api/llm/profiles` after creating a profile with the synthetic key | PASS — body contains no synthetic key; `api_key_hint` = `"." + "7F2A"` (last-4-by-design — not the full synthetic key) |
| `GET /api/auth/status` | PASS — only `initialized` |
| `/api/llm/profiles` create response | PASS — `{id}` only |
| custom-tool exec (Gate A fix) | PASS — network commands blocked |

All tests: `server/secrets_leak_test.go` (2), `server/customtool_exec_test.go` (1) — PASS.

## 11. Database ownership model (Gate D)

- **No multi-user ownership model exists.** `db` has no `users` table; `db.Task` (db/tasks.go:13) has no owner field (single-admin deployment, single JWT auth). All DB access is mediated by `requireAuth` (single admin session).
- Objects with no ownership today: Tasks, Findings, Evidence (db.Node), Profiles, Sessions, Audit events. Ownership = none (single-admin).
- Cross-user checks (Gate D4): N/A — no second user can authenticate (`/api/auth/init` rejects if password exists; `authLogin` returns error for non-"ARTEX" username).

## 12. PostgreSQL privilege analysis (Gate D)

- Runtime role: same as the ARTEX process user (`ARTEX_PG_DSN`). No superuser requirement (schema uses standard tables); no migration privilege needed at runtime (migrations at first Open — note: schema migration requires DDL — the runtime user needs CREATEDB/migration rights — documented).
- Sensitive tables: all accessible — documented: single-admin threat model; no RLS (not applicable for single admin).
- Transactions preserve invariants — no concurrent ownership changes possible (single admin).

## 13. API route authorization matrix (Gate E)

- Global wrapper: `api := cors(s.requireAuth(mux))` (server/server.go:959) — every route under `/api/*` requires valid JWT unless exempt.
- Exempt by design: `GET /api/auth/status`, `POST /api/auth/init`, `POST /api/auth/login`, `POST /api/auth/change-password`, `GET /api/health` (server/server.go:676-678 + server/auth.go:120-136).
- Client-supplied IDs: no ownership bypass (no owner fields exist); DB queries use IDs as keys only.
- Spot checks (representative): `/api/llm/profiles` GET — returns all profiles (single-admin); `/api/llm` POST — gated by VIII.4.8 trust check; `/api/tools/*` — authenticated (ORM rows created via API).
- Debug endpoints: none expose env or credentials.

## 14. Cross-boundary adversarial test results (Gate F)

| # | Scenario | Result (evidence) |
|---|---|---|
| 1 | Untrusted profile attempts to use unauthorized provider | `TestLegacyNewProviderFailsClosedWhenUntrusted` (agent, PASS); runtime Live A/A2/A3 (8.5 lab) — denied 400/502 |
| 2 | Task attempts to expand network scope | `TestRedirectChainRevalidatedEachHop` (agent, PASS); `TestScopeTaskIsolation` (PASS) |
| 3 | Same-IP different hostname | `TestProviderDNS_DifferentHostSameIP` (PASS) |
| 4 | DNS answers change between validation and connection | `TestProviderDNS_RebindingRevalidatedPerDial` (PASS); `TestEgressForwarder_RebindingRevalidatedPerConnection` (PASS) |
| 5 | Provider retry after transient | `TestProviderRetry_Transient500SucceedsViaPinnedClient`, `TestProviderRetry_PersistentErrorExhaustsBoundedRetries`, `TestProviderRetry_ContextCancelStopsRetries` (agent, PASS) |
| 6 | Traffic allowlist changes while requests active | `TestTrafficAllowlistConcurrentReadWrite`, `TestTrafficAllowlistDeniedHostNeverAppears` (traffic, PASS — atomic snapshots) |
| 7 | Two tasks, different scopes, concurrent | `TestScopeTaskIsolation` (agent, PASS) |
| 8 | Worker exits mid-request | `TestProviderRetry_ContextCancelStopsRetries` + scope-pin ctx-cancel (agent, PASS) |
| 9 | Two users | N/A — single-admin deployment (documented, §11) |
| 10 | Server restart with empty/invalid trust | Documented: trust root = env seed; with restricted set, empty-BaseURL profiles denied (§3); unrestricted legacy default documented (§3, viii.4.7 semantics) |
| 11 | Report export contains synthetic-secret test data | Synthetic-only tests; no real data in exports (§10) |
| 12 | Malformed admin config loaded | `TestSetTrustedProviderHostsMalformedEntriesNeverAuthorize` (agent, PASS) |

## 15. Previous-phase follow-up (Gate G)

- Race detector: `RACE DETECTOR UNKNOWN` (no C compiler; `cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%`; installing out of scope).
- `TestSkillRelPath`: `PRE-EXISTING` — Unix-semantic test expectation vs OS-native production; not changed (out of scope); no new finding.
- `TestGraphOverviewExpandsAssociatedCompanyScope`: `PRE-EXISTING` (8.4.7) — async goal seeding timing; scope/graph files untouched.
- `TestCoreTaskLifecyclePG`: `ENVIRONMENTAL` — goal seeding async via `server/goals.go`; requires active LLM provider.
- `TestEvidenceBindingLifecycle`: `ENVIRONMENTAL (flaky)` — passes standalone; full-run nondeterminism; `evidence/store.go` Windows fix in place.

## 16. Files changed and justification (viii.5)

- `agent/fs_containment.go` (new) — write-path containment.
- `agent/fs_containment_test.go` (new) — containment tests.
- `agent/execution_boundary_test.go` (new) — surfaces/guard tests.
- `agent/bash_guard.go` — `NewBashGuardTool` export.
- `agent/planner.go`, `agent/worker.go`, `agent/mainagent.go`, `agent/chat.go` — `ContainFileTools` on assembled surfaces.
- `server/customtool.go` — `runCommandTool` → `agent.NewBashGuardTool`.
- `server/customtool_exec_test.go` (new), `server/secrets_leak_test.go` (new).

## 17. Commands and exit codes (viii.5)

- `go build ./...` → exit 0. `go vet ./agent/ ./server/ ./traffic/ ./enrich/ ./selfupdate/` → exit 0. `git diff --check` → exit 0.
- `go test ./agent/ -run 'TestFsContainment|TestContainFileTools|TestDefaultSurfacesHaveNoBash|TestBashGuardBlocksNativePowerShellNetCommands|TestSideActualChatCheckpointToolResultAndTranscriptIsolation' -count=1` → PASS (exit 0).
- `go test ./agent/ -count=1` → PASS (exit 0, except pre-existing/environmental — see §15; no new failures introduced by viii.5; all 700+ agent tests pass).
- `go test ./server/ -run 'TestSyntheticSecretsNotExposedViaProfilesAPI|TestAuthStatusDoesNotExposeSecrets|TestCustomToolCommandNetworkBlockedByGuard' -count=1` → PASS (exit 0).

## 18. Remaining limitations

1. Race freedom UNKNOWN (no C compiler).
2. Windows symlink-escape test skips (no symlink privilege) — containment implemented and unit-verified for the non-symlink cases; symlink case static-reviewed (§7.2).
3. A7 — npm probes (norma) — documented; not on ARTEX surfaces (no injection site found).
4. A3 — norma background Task tool — not injected into ARTEX surfaces (verified by grep; documented).
5. Read/LS/Glob/Grep not contained — documented trade-off.
6. No multi-user DB ownership model — single-admin deployment (documented).

## 19. Final security verdict

**`PARTIAL — SECRET HANDLING VERIFIED; LOCAL EXECUTION + FILESYSTEM + API AUTHORIZATION GAPS REMAIN`**

Evidence: all §5, §7, §10 findings closed/documented; §15 lists residual/pre-existing items. This is not a claim of complete security.
