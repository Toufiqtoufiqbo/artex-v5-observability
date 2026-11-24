# PHASE8_4_1 â€” SOURCE PRESERVATION

Date: 2026-11-21 (local, WAT/UTC+1)
Repo: `<REPO_ROOT>`

Mandatory precondition for this phase: capture branch, HEAD, full status, binary diff, untracked inventory, hashes of modified files, and a recoverable backup **before** any edit.

## 1. Captured state (executed)

```
branch:  (detached HEAD)
HEAD:    e6ec56999175509551a1e43a3ee3b053d0941bd9

git status --short:
 M agent/tools.go
 M agent/worker.go
?? agent/finding_gate.go
?? agent/finding_gate_test.go
?? agent/http_connection_counter_test.go
?? agent/http_redirect_test.go
?? agent/httpclient.go
?? agent/scope_failclosed_test.go
?? agent/scopecheck.go
?? agent/scopecheck_test.go
?? agent/strategy.go
?? agent/tools_attackpath.go
?? agent/tools_attackpath_test.go
?? agent/tools_diff.go
?? agent/tools_diff_test.go
?? agent/tools_http.go
?? agent/tools_login.go
?? agent/tools_login_test.go
?? agent/tools_probe.go
?? agent/tools_probe_test.go
?? agent/tools_retest.go
(+ further entries as reported by the tooling; complete copy: backups\phase8_4_1\git-status-short.txt)

git diff --binary â†’ backups\phase8_4_1\git-diff-binary.txt
git ls-files --others --exclude-standard â†’ backups\phase8_4_1\untracked.txt
```

Full status, binary diff, and untracked list preserved as files under `backups\phase8_4_1\`.

## 2. Hashes of files intended for modification (captured before editing)

```
5AA0E858737F9E0949E18A221E669AE3248C55A9FEC8654D3D79152986BEB0CB  agent\scopecheck.go
2DDDDE71C4FBF54DEB14F8BD9A6435A9E76D7667A68A91B9917A8964D3A1E73E  agent\scopecheck_test.go
23F50101BCC6A0B6F79B498A41796A56AA59CF963913CC28F0C3564B6401BBB2  agent\http_connection_counter_test.go
(others recorded for the complete set of files that were touched this phase)
```

## 3. Backup (recoverable, inspectable)

```
backups\phase8_4_1\
â”œâ”€â”€ HEAD.txt
â”œâ”€â”€ BRANCH.txt
â”œâ”€â”€ git-status-short.txt
â”œâ”€â”€ git-diff-binary.txt
â”œâ”€â”€ untracked.txt
â”œâ”€â”€ task_scope.go                     (pre-phase copy)
â””â”€â”€ agent-snapshot\                   (pre-phase copy of the whole agent/ package, 70 files)
```

Backup integrity check performed: files exist and are readable (Get-ChildItem / Get-FileHash on the snapshot produced hashes consistent with the pre-phase state).

## 4. Preserved-existing-changes verification (post-edit)

After the phase, re-ran `git status --short` and confirmed the pre-existing dirty files are still present and not silently discarded:

```
 M agent/tools.go
 M agent/worker.go
```

Both remain modified as before. No `git reset --hard`, `git clean`, `git checkout`, `git stash`, dependency upgrade, or destructive DB operation was run anywhere in this phase.

## 5. Baseline archive untouched

The published baseline tree was **not** modified in this phase (the phase's mission is "only the modified ARTEX v5 source tree is in scope"). Baseline SHA-256 `93AFBEC0â€¦CD11E2` re-verified after the phase.

## 6. Result

**SOURCE PRESERVATION: PASSED.** All pre-existing modifications and untracked files preserved; backup exists and is inspectable.
