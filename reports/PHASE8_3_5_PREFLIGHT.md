# PHASE8_3_5 â€” PREFLIGHT

Date: 2026-11-21 (local, WAT/UTC+1)
Scope: local-only. No external targets. Secrets redacted.

## 0. Initial state (session restart)

At the start of this phase **all four services were down** (`8888/8787/3004/5433` DOWN, health unreachable). Nothing was overwritten silently; state was re-established and recorded below.

## 1. Restoration actions (recorded)

| Step | Command (abridged) | Result |
|---|---|---|
| PostgreSQL start | `pg_ctl -D pgdata -o "-p 5433" start` | listening 127.0.0.1:5433 (pid 4120) |
| Fixture start | `python bench-vuln.py 3004` | `bench-vuln on 127.0.0.1:3004` |
| Baseline build | `go build -o artex-baseline-8332.exe ./cmd/artex` | exit 0, 43,968,512 bytes |
| Baseline start | `artex-baseline-8332.exe -addr 127.0.0.1:8888 -proxy 127.0.0.1:8889 -data artex-base-pilot-data` | listening 8888 |
| Modified start | `artex-original-embed-v5.exe -addr 127.0.0.1:8787 -proxy 127.0.0.1:8788 -data artex-v5-pilot-data` | listening 8787 |

Note: `pg_ctl` first started on the config default 5432; it was stopped and restarted with `-p 5433` to match the documented environment. This is an environment action, not a source change.

Go toolchain: `go1.26.11 windows/amd64` (`<GO_BIN>\go.exe`).

## 2. Isolation matrix

| Item | Baseline | Modified v5 |
|---|---|---|
| HTTP port | 127.0.0.1:8888 | 127.0.0.1:8787 |
| Proxy port | 127.0.0.1:8889 | 127.0.0.1:8788 |
| Database | `artex_base_pilot` | `artex_pilot` |
| Data dir | `artex-base-pilot-data` | `artex-v5-pilot-data` |
| Log | `baseline-pilot.log` | `v5-pilot.log` |
| Binary | `artex-baseline-8332.exe` (rebuilt) | `artex-original-embed-v5.exe` |
| Admin user | `ARTEX` | `ARTEX` |

Both databases were created fresh for this phase (`DROP DATABASE IF EXISTS` â†’ `CREATE DATABASE`). Older databases (`artex`, `artex_base`) are **preserved untouched** for prior-phase evidence.

## 3. Contamination discovered and corrected

The pre-existing modified database (`artex`) contained **79 tasks** and two leftover agents (`lock_agent_*`, `write_lock_agent_*`). On restart the engine resumed **6 stale tasks** (104, 127, 215, 216, 217, 222) and consumed the shared rate limit:

```
2026/11/21 20:37:22 [manager] reloaded 79 task(s) from PG
2026/11/21 20:37:23 [worker work#1] task 223 é¢†å–æ„å›¾ #1964
...
4458|216|worker|40284|15593|53|ok|2026-11-21 20:41:12
```

This makes paired conditions impossible, so **both** versions were moved to fresh, symmetric pilot databases and restarted. Recorded, not hidden.

## 4. Provider configuration (effective, key redacted)

Applied via authenticated `POST /api/llm` (admin login first, API key supplied through an environment variable, all printed output scrubbed):

| Field | Baseline | Modified v5 |
|---|---|---|
| provider | openai | openai |
| model | `nvidia/nemotron-3.5-lightning-30b-a3b` | `nvidia/nemotron-3.5-lightning-30b-a3b` |
| base_url | `https://integrate.api.nvidia.com/v1` | `https://integrate.api.nvidia.com/v1` |
| rate_per_second | 0.3 | 0.3 |
| rate_per_minute | 18 | 18 |
| context_window_k | 128 | 128 |
| key_set | true | true |
| setLLM / getLLM | 200 / 200 | 200 / 200 |

## 5. Agent/provider binding check

`SELECT key,llm_profile_id FROM agents WHERE llm_profile_id IS NOT NULL;` â†’ **0 rows** in both databases.
Therefore planner and workers resolve through `globalProvider()` (`server/task_llm.go:509`), i.e. exactly the configuration above. No hidden per-agent override.

## 6. Regression observed during setup

On a **fresh** database, modified v5 rejects login with `403 {"error":"å¯†ç æœªåˆå§‹åŒ–ï¼Œè¯·å…ˆè®¾ç½®å¯†ç "}` (password not initialized). The baseline binary has `ARTEX_ADMIN_PASSWORD` auto-init logic (`server/auth.go:36`), while the modified tree has **no** `ARTEX_ADMIN_PASSWORD` reference:

```
# baseline
auth.go:36 adminPasswordEnv = "ARTEX_ADMIN_PASSWORD"
# modified: (no matches)
```

For the pilot, modified v5 was initialised explicitly via `POST /api/auth/init` (`200`). This is a genuine capability difference and is carried into the regression review; it does not affect detection logic.

## 7. Health

| Service | Endpoint | Result |
|---|---|---|
| Baseline | `GET http://127.0.0.1:8888/api/health` | 200 `{"ok":true,"service":"artex","version":"dev"}` |
| Modified | `GET http://127.0.0.1:8787/api/health` | 200 `{"ok":true,"service":"artex","version":"dev"}` |
| Fixture | `GET http://127.0.0.1:3004/ok` | 200 `ok` |
| PostgreSQL | TCP 5433 | listening |

## 8. Source identity

| | Baseline | Modified |
|---|---|---|
| Source | `artex-global-main.zip` â†’ extracted tree | `artex-original` git tree |
| Archive SHA-256 | `93AFBEC089A9D8AB00AE1DA4918DFCAF3A99AF636C1303DAE514F4EBE7CD11E2` | â€” |
| Archive size | 8,944,269 bytes | â€” |
| Commit | **unverified** (no git metadata in archive) | `e6ec56999175509551a1e43a3ee3b053d0941bd9` (detached HEAD) |
| Working tree | clean | modified + untracked (documented) |
| Files / manifest SHA-256 | 794 / `1D7969A0D8487129613233175F03498E79F1840D65F220AEA6042FC86D5748DD` | 2120 / `4BF9796F7F773505FCFC17614021F4F8CF5E6302828C8ACC92348B8EA2EF204D` |
| Go directive | 1.26.3 | 1.26.3 |
| norma | v0.4.4 | v0.4.3 |

Manifest rule: SHA-256 over sorted `relpath<TAB>filehash` for every file, excluding `.git/`, `node_modules/`, `webui/dist/`.

Preserved limitations: baseline commit identity unverified; relationship to `v0.3.15` unverified; `norma` skew v0.4.4 vs v0.4.3.
