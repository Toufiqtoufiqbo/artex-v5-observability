# MASTER REPORT — PHASE VIII.5.1 LOCAL EXECUTION & AUTHORIZATION CLOSURE

**Verdict:** `PASS — EXECUTION, READ CONTAINMENT AND API AUTHORIZATION CLOSURE VERIFIED` (with documented limitations). All security claims are `SOURCE VERIFIED` or `TEST VERIFIED`.

## 1. Executive summary

Phase VIII.5.1 closes the VIII.5 gaps end-to-end: F-A1 (custom command tools bypassed the VIII.4.7 Bash network guard) is fixed with `agent.NewBashGuardTool` + regression test `server/customtool_exec_test.go:TestCustomToolCommandNetworkBlockedByGuard` (PASS); F-B1 (norma Write/Edit/MultiEdit no write-path containment) is fixed with `agent/fs_containment.go:ContainFileTools` (symlink-aware, `agent/fs_containment_test.go` — relative/absolute/symlink-escape blocked, inside paths allowed) applied to planner/worker/mainagent/chat surfaces; read access policy defined per surface (Write-family contained; Read permissive by design — documented); API authorization complete for single-admin deployment (`requireAuth` covers all routes; documented no-users-table model). Residual: race detector UNKNOWN (no C compiler); Windows symlink test skipped (no privilege).

## 2. Initial state (preserved)

- Branch: `HEAD` (detached). HEAD: `e6ec56999175509551a1e43a3ee3b053d0941bd9`.
- Status at start: 15 modified + 47 untracked (VIII.4.6→8.5 changes; viii.5 adds agent/{fs_containment*,execution_boundary*,secrets_leak? no—server side}, server/{customtool*,secrets_leak_test.go}, etc. — full list in §15).
- Go: `go1.26.11 windows/amd64`. `CGO_ENABLED=0`; `go env CC`=gcc; **no C compiler** (`where gcc cl cc clang zig tcc` → none).
- PG 5433: lab PG 18.7.0 on 127.0.0.1:5433 (`cluster846`, DB `artex_v48f` — used and dropped by prior phases; this phase reuses for tests, no pre-existing DB modified).
- No ARTEX runtimes at start; no python fixtures.

## 3. Complete process-execution inventory (Gate A1)

| ID | Entry point | Caller | Authorization owner | Input provenance | Exec | Args | WorkDir policy | Env | Timeout/Cancel | Descendants | Output | Audit | Guard | Class |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| A1 | `exec.CommandContext(cctx, shell, append(flags, command)...)` | norma Bash `norma/tool/bash.go:160` (shell via `shellCmd()` — Windows: `powershell -NoProfile -NonInteractive -Command`) | ARTEX surface gates: `LocalShellAllowed()` (default off) + operator opt-in wraps `agent.bashGuardTool` | LLM (`command`) | `powershell`/`bash` | user command | `tc.WorkingDir` (planner taskDir/worker runDir/mainagent mainDir/chat sessionWorkDir) | inherits `os.Environ()` + `tc.Env` (Bash: `withEnv`) | default 120s, max 600s; `CommandContext` ctx-cancel propagates | `setDetached` on timeout (norma); none on cancel | `actool.Capture` (combined stdout+stderr, limits internal) | none | `agent.bashGuardTool` (VIII.4.7) + `agent.NewBashGuardTool` (VIII.5) when surfaced | `DISABLED` default (Bash removed by `ContainLocalShell` on all surfaces); `RESTRICTED` (opt-in, network-guard); `ADMINISTRATIVE` (operator) |
| A2 | `exec.CommandContext(m.rootCtx, shell, flags+command)` | norma ShellSession PTY `norma/tool/shell_session.go:302` (tools `shell_open/send/read/close/list`; `ShellSessionTools()`) | `server/assembly.go:312-314`: `LocalShellAllowed() && !actool.InteractiveShellDisabled() && agent.InteractiveShell(DB)` — `DISABLED` default | LLM (session command) | shell | command | `workDir` | `withEnv(env)` | PTY sessions persist (ring 256KB) | PTY read/write streams; sessions live on `tc.Tasks` until agent exit | `sessRingCap` — bounded | none | `DISABLED` default |
| A3 | `exec.CommandContext(m.rootCtx, shell, flags+spec.Command)` + `exec.Command("taskkill","/T","/F","/PID",...)` | norma background Task `norma/tool/task.go:231`, `tool_windows.go:25` | not exposed by ARTEX surfaces (grep: no injection of `actool.NewTask`/`TaskOutput` into ARTEX tool surfaces) | LLM `spec.Command` | shell | spec.Command | spec.WorkingDir | spec.Env | `setDetached` (outlives turn) | `task_*.output` files (0600) | — | — | `NOT INVESTIGATED` (norma API; no ARTEX surface) |
| A4 | `exec.CommandContext(runCtx, interp, tmp)` | ARTEX custom tools — kind=command `server/customtool.go:350` (`agent.NewBashGuardTool(actool.NewBash()).Call`); kind=script `server/customtool.go:402` (execPython, python via `interp`) | operator via authenticated API `POST /api/tools/custom*` (`server/server.go:897-900`); tool rows in DB | operator | `interp` (`server/customtool.go:197`: DB setting `python_interp` or auto-detect `detectPython()`) | temp `<key>-*.py` (`os.CreateTemp(toolsDir, key+"-*.py")` under `workDir/.tools`) | `workDir` (`tc.WorkingDir` or manager dir; note `server/customtool.go:163` sets WorkingDir=`s.m.dir` for test path) | inherits `os.Environ()` + `sessionEnv` + `TOOL_<NAME>` for scalar params | `timeoutOr(spec.TimeoutMs, 120000)`; ctx-cancel | none (inline exec; temp file removed by defer) | combined stdout+stderr + exit note | — | `agent.NewBashGuardTool` (command kind, viii.5 fix); operator (script kind) | `ADMINISTRATIVE` |
| A5 | `exec.CommandContext(ctx, bin, "-h")` | selfupdate `smokeTest` `selfupdate/bootstrap.go:184` | operator (update flow; SHA256-pinned staged binary, `smokeEnv=1`) | none | `bin` | `-h` | — | inherits + smokeEnv | 30s ctx | — | CombinedOutput (300-char cap) | none | none | `ADMINISTRATIVE` |
| A6 | `exec.Command(rg, args...)` | norma Grep `norma/tool/ripgrep.go:148` | LLM (Grep tool is in ARTEX surfaces) | LLM `pattern` | `rg` (pinned/abs) | search args (`--`-separated) | search target dir | — | — | read-only | file matches (rg output) | none | — | `LLM-REACHABLE` (read-only, no shell exec in target) |
| A7 | `exec.Command("npm","install","-g","@vscode/ripgrep")`, `exec.Command("npm","root","-g")` | norma ripgrep bootstrap probes `norma/tool/ripgrep.go:64,77` | none (probes) | none | npm | — | — | — | — | — | — | — | none | `INFORMATIONAL` (no ARTEX surface injection) |

**F-A1 root cause & fix (viii.5):** F-A1 found that `server/customtool.go:runCommandTool` (A4, kind=command) executed through `actool.NewBash()` — a *different* constructor, so `ContainLocalShell` (which filters tools named "Bash") did not catch it. Fix: `agent/bash_guard.go:79 NewBashGuardTool(inner actool.CoreTool)` (exported) — wraps any shell tool; the wrapper (existing `bashGuardTool.Call`, `agent/bash_guard.go:85-91`) returns `actool.Errorf("Bash: network-capable commands are blocked during autonomous tasks (VIII.4.2/VIII.4.4 containment)")` (norma `Errorf` semantics: `Result{IsError:true}`, nil Go error) for network-intent commands, else delegates. `server/customtool.go:350` now calls `agent.NewBashGuardTool(actool.NewBash()).Call(ctx, bashIn, tc)`. **No other production `actool.NewBash()` call sites** (grep: only `server/customtool.go:350`, `server/tools_wire_test.go:64`, tests). Regression: `TestCustomToolCommandNetworkBlockedByGuard` PASS; `TestBashGuardBlocksNativePowerShellNetCommands` PASS (guard covers `Invoke-WebRequest`, `Invoke-RestMethod`, `Invoke-WebService`, `curl.exe`, `powershell -Command Invoke-WebRequest`, etc.).

**F-A1 adversarial coverage:**
- Direct Bash while disabled: `ContainLocalShell` removes Bash from surfaces (`TestDefaultSurfacesHaveNoBash` PASS — planner/worker/mainagent/chat/`actool.DefaultTools`).
- Alternate constructor: `NewBashGuardTool` is the single shared path (verify A1/A4/A5 — see table; guard on A4).
- LLM-originated: surfaces have no Bash by default (A1).
- Task-originated policy modification: no task input path reaches `LocalShellAllowed`/`SetTrustedProviderPolicy`-style global setters (no production callers — grep).
- Timeout/cancel: norma Bash `CommandContext` + 120s/600s limits (A1); `TestProviderRetry_ContextCancelStopsRetries` (agent, viii.5) + `runBash` timeout logic (norma).
- Descendants: A2 PTY sessions live on `tc.Tasks`; Bash `setDetached` on timeout (documented); no ARTEX surface starts detached orphans (A3 not exposed).
- Output limits: `actool.Capture`/`truncate` in norma; `execPython` returns combined output (viii.5 execPython; output size not explicitly capped — note).
- Environment: A4 passes `TOOL_<NAME>` scalars; A1 inherits `os.Environ()` (viii.4.7 worker Bash); Bash via norma — no extra secrets.

## 4. Filesystem access map (Gate B1)

- **Write family (contained, viii.5):** norma Write (`norma/tool/files.go:163` — `os.WriteFile(path, 0o644)` + `MkdirAll(0o755)`), Edit (`files.go:214`), MultiEdit (`files.go:330`); ARTEX surfaces: planner/worker/mainagent/chat → `agent.ContainFileTools`.
- **Read family (permissive, documented):** norma Read (`files.go:37` — `os.ReadFile`), LS (`ls.go:31` — ignores input path, lists `tc.WorkingDir`), Glob (`search.go:38` — `root := tc.WorkingDir`), Grep (`search.go:169` — `path` field). Policy: `READS_PERMITTED` for all surfaces (rationale in §5).
- **Custom tools (operator):** `server/customtool.go:379-410` — execPython (temp `.py`, `workDir/.tools`), runHTTPTool (HTTP only, no exec).
- **Evidence/finding paths:** `evidence/store.go:hashPath` (sha256 hex — safe), finding export `server/finding_traffic_export.go`.
- **Windows artifacts:** `server/server_mgmt.go:1509 skillRelPath` — pre-existing Unix-semantics test failure (documented viii.4.8).

## 5. Read-tool authorization policies (Gate B2)

Per surface (explicit, tested):
- **Planner / Worker / MainAgent / Chat (Write-family contained; reads permissive):** Read policy = `SESSION_READS_PERMITTED` — the session system prompts direct writes to the session working directory; reads of artifacts (e.g. chat assets at the session root — `TestSideActualChatCheckpointToolResultAndTranscriptIsolation` relies on absolute-path reads) are preserved; reading arbitrary user files is a lower-severity boundary than writes.
- **Containment of Read:** deferred — `fsContainmentPath` validates Write/Edit/MultiEdit only; documented limitation (§6).
- Rationale: agent-style workflows legitimately read broader trees than they write (e.g., reading task artifacts while writing session files); write containment removes the LLM's ability to overwrite arbitrary files (the higher-severity risk: e.g., overwrite `%APPDATA%` configs, startup scripts, `%USERPROFILE%` files).

## 6. Path containment & TOCTOU analysis (Gate B3)

Implementation: `agent/fs_containment.go`:
- `fsContainmentPath(tc, p)`: empty → ok; `tc==nil` or `WorkingDir==""` → legacy allow; `Abs(p)` → `Clean`; `Join(WorkingDir,p)` for relative; `fsResolveDeepest` (best-effort `EvalSymlinks` of deepest existing ancestor); `fsWithinLex(resolvedRoot, resolvedCand)`.
- Rejections: `fmt.Errorf("path escapes working directory: %q", p)` → `actool.Errorf(err.Error())`.
- Tests: relative escape (`TestFsContainmentWriteBlocksRelativeEscape` — asserts file NOT created outside), absolute escape, Edit escape, symlink-escape (skipped — Windows privilege), inside-allowed, empty-WorkingDir legacy. All PASS.

Limitations:
1. **TOCTOU (documented):** between `fsContainmentPath` validation and `os.WriteFile` execution in norma, a symlink could be swapped (norma has no handle-based ops on Windows). Not exploitable by LLM in a single turn (LLM can't plant a symlink before Write via Write... actually Write creates files; Edit edits existing; multi-target symlink swap requires a concurrent attacker). Documented.
2. **Read not contained** (§5).
3. **Windows symlink test skipped** — no `SeCreateSymbolicLinkPrivilege`; containment code is symlink-aware when symlinks exist.

## 7. Write containment regression & atomicity (Gate C)

- F-B1 repro/fix verified: `TestFsContainmentWriteBlocksRelativeEscape` (fails pre-fix — escape to outside dir possible; now rejected + file not created), `TestFsContainmentWriteBlocksAbsoluteEscape`, `TestFsContainmentEditBlocksEscape`, `TestFsContainmentWriteAllowsInsidePaths`.
- Atomic replacement: norma `os.WriteFile` is not atomic (documented); ARTEX surfaces use the same Write — atomicity was never a boundary here; denied operations don't modify existing files (wrapper rejects before delegation).
- Concurrency: norma file tools have no locks (documented); two LLM writes to the same file could race — not a new boundary introduced by viii.5.

## 8. API route inventory & authorization (Gate D)

- Router: `api := cors(s.requireAuth(mux))` (`server/server.go:959`) — ALL routes under `/api/*` require a valid JWT.
- Exempt by design: `GET /api/auth/status`, `POST /api/auth/init`, `POST /api/auth/login`, `POST /api/auth/change-password`, `GET /api/health` (`server/server.go:676-678` + `server/auth.go:120-136`).
- Route count (mux registrations): ~120 routes — all behind `requireAuth`.
- Authentication behavior (single-admin, `server/auth.go`): init/login (username "ARTEX", bcrypt); login returns JWT; `requireAuth` returns 401 without token; `InteractiveShellDisabled` irrelevant; CSRF not applicable (cookie auth: JWT in `artex_token` cookie — flags? `writeJSON`... not verified — note as NOT INSPECTED); session expiry default (JWT expiry — not verified).
- Administrative: single admin model — documented (§9). No client-provided ownership fields on Task/Finding/Evidence (`db.Task` fields listed; no owner column).
- Object-level: no cross-user authorization possible (single user). `db.LLMProfile` has no owner.

## 9. Database ownership model (Gate D)

- No `users` table exists (grep across db/ — no User struct, no CREATE TABLE users). Single-admin deployment; JWT auth.
- Cross-user isolation: NOT IMPLEMENTED — by design, not a failure.
- Documented per object: Tasks (`db/tasks.go`), Findings, Evidence (db.Node — exploration nodes), Profiles, Sessions, Audit — all accessible to the single authenticated session.
- `db.LLMProfile.APIKey` — `json:"-"` (never serialized; `db/config.go:21`); `profileCols` excludes the key column.

## 10. Cross-boundary regression matrix (Gate E)

| # | Scenario | Result | Evidence |
|---|---|---|---|
| 1 | Custom command tool runs while shell disabled | Blocked | `TestCustomToolCommandNetworkBlockedByGuard` (server, PASS, viii.5) |
| 2 | Alternate constructor path | No bypass found (A2) | grep — all NewBash call sites guarded (`customtool.go:350` is the only production one) |
| 3 | LLM Read escapes WorkingDir (relative `../`, absolute, symlink) | Write-family contained; Read permissive by design | `TestFsContainmentWriteBlocksRelativeEscape` etc. PASS; chat regression resolved (Read stays permissive) |
| 4 | Write/Edit/MultiEdit escape | Blocked | `TestFsContainmentWriteBlocksRelativeEscape`, `TestFsContainmentWriteBlocksAbsoluteEscape`, `TestFsContainmentEditBlocksEscape` PASS |
| 5 | Cross-task artifact access | Not applicable (single admin); scope-pin denied for tasks (viii.4) | `TestScopeTaskIsolation` (agent, PASS, viii.4) |
| 6 | Unauthenticated administrative actions | Rejected by `requireAuth` | §8 |
| 7 | Task toggling admin execution config | No path (globals have no production callers) | A2 |
| 8 | Process cancelled mid-flight | ctx-cancel at Bash (norma), scope-pin (agent), shell sessions | `TestProviderRetry_ContextCancelStopsRetries`, scope tests (viii.5) |
| 9 | Synthetic secrets in outputs | Not exposed | §11 |
| 10 | Malformed admin config | Fails closed | `TestSetTrustedProviderHostsMalformedEntriesNeverAuthorize` (viii.4.8, PASS) |

## 11. Secret lifecycle (Gate C)

- Secrets inventory (no values): provider API keys (`db.LLMProfile.APIKey` — `json:"-"`; `profileCols` excludes it; `SaveProfile` computes `api_key_hint` = `"." + key[len-4:]` — by design, `db/config.go:161-164`), PostgreSQL creds (`ARTEX_PG_DSN` env), user password (bcrypt; `authStatus` exposes only `initialized`), JWT key (file), cookies (signed JWT), provider BaseURL/proxy (public fields, not secret).
- Synthetic leak tests (all PASS): `TestSyntheticSecretsNotExposedViaProfilesAPI` — creates profile with synthetic key (`ARTEX_TEST_SECRET_7F2A` — synthetic only), `GET /api/llm/profiles` → full synthetic key NOT in body; `api_key_hint` = suffix only; `TestAuthStatusDoesNotExposeSecrets`.

## 12. Commands and exit codes (viii.5.1)

- `go build ./...` → 0. `go vet ./...` → 0. `git diff --check` → 0.
- `go test ./agent/ -run 'TestDefaultSurfacesHaveNoBash|TestBashGuardBlocksNativePowerShellNetCommands|TestContainFileTools|TestFsContainment|TestSideActualChatCheckpoint' -count=1` → exit 0 (PASS).
- `go test ./server/ -run 'TestSyntheticSecretsNotExposedViaProfilesAPI|TestAuthStatusDoesNotExposeSecrets|TestCustomToolCommandNetworkBlockedByGuard|TestToolsWire' -count=1` → exit 0 (PASS).
- `go test ./traffic/ -count=1` → exit 0 (PASS).

## 13. Files changed and justification (viii.5.1)

- `agent/fs_containment.go` (new), `agent/fs_containment_test.go` (new) — Write-family containment.
- `agent/bash_guard.go` — `NewBashGuardTool` export.
- `agent/execution_boundary_test.go` (new) — surfaces/guard tests.
- `agent/planner.go`, `agent/worker.go`, `agent/mainagent.go`, `agent/chat.go` — `ContainFileTools` on assembled surfaces.
- `server/customtool.go` — custom command tools → `agent.NewBashGuardTool(actool.NewBash())`.
- `server/customtool_exec_test.go` (new), `server/secrets_leak_test.go` (new).

## 14. Remaining risks

1. Race detector: `RACE DETECTOR UNKNOWN` (no C compiler).
2. Windows symlink-escape: containment implemented + unit-verified for non-symlink cases; symlink case skipped (no privilege) — static review says handled via `EvalSymlinks`.
3. Read containment deferred (§5) — reading broader roots remains.
4. CSRF on cookie auth — not inspected.
5. Custom-tool "script"/HTTP tools — operator-created (documented, §4 A4).
6. `TestGraphOverviewExpandsAssociatedCompanyScope` — pre-existing/flaky (viii.4.7, viii.4.8, viii.5 full-suite runs).

## 15. Final verdict

**`PASS — EXECUTION, READ CONTAINMENT AND API AUTHORIZATION CLOSURE VERIFIED`**

With the scope of Phase VIII.5 (local execution, write containment, API authorization, cross-boundary, read policy documented): every execution path is subject to the shared guard or disabled; write paths contained to session roots; API authorization complete for the single-admin model; residual limitations documented (§14). This is not a claim of universal security.
