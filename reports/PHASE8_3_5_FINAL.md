# PHASE8_3_5 — FINAL

Date: 2026-11-21 (local)
Scope: local-only. No external targets. No secrets in this report.

## Verdict

# `PAIRED PILOT COMPLETED — EXPLORATORY`

Both versions performed genuine model inference through their own Planner/Worker pipelines against the identical local fixture, and one paired run completed under verified, equivalent conditions. This does **not** establish general superiority.

## 1. Answers to the required questions

| # | Question | Answer |
|---|---|---|
| 1 | Did both versions perform genuine Planner inference? | **Yes.** Baseline 7 planner calls, modified 5 planner calls, all `status=ok` with token counts in `llm_usage`. |
| 2 | Did both use the intended provider? | **Yes.** `openai`-compatible client → `https://integrate.api.nvidia.com/v1`; startup log `[engine] LLM configured via UI: openai / nvidia/nemotron-3.5-lightning-30b-a3b` on both. |
| 3 | Was model identity verified? | **Partially.** The model string is recorded per call (`llm_usage.model = nvidia/nemotron-3.5-lightning-30b-a3b`), the configured model matches, and every call returned real token usage. Provider-side `response.model` echo / request-ID is **not** surfaced (`llm_records` raw capture was empty), so this is application-level verification, not provider-attested. Limitation preserved. |
| 4 | Did Workers perform inference? | **Yes, both.** Baseline 6 worker calls, modified 14 worker calls, same model. |
| 5 | Was one comparable local test case selected? | **Yes.** Boolean SQLi on `GET http://127.0.0.1:3004/search?q=`, ground truth `bench-truth.json` id 1. |
| 6 | Was fixture reset verified? | **Yes.** Hits log truncated (727 → 0; then 0 before each run), fixture is stateless, all 6 ground-truth positives re-probed. |
| 7 | Did both paired runs complete? | **Yes.** Baseline `done` in ~200 s; modified `done` in ~160 s. |
| 8 | Were findings supported by real evidence? | **Yes, both.** Evidence corresponds to requests actually present in `bench-hits.log`, and the 56-vs-0 differential reproduces independently. |
| 9 | Is Phase VIII.3.6 ready? | **Ready with limitations** (see §6). A larger benchmark is possible, but must be framed against the base-version divergence. |

## 2. Source identities

| | Baseline | Modified v5 |
|---|---|---|
| Source | `artex-global-main.zip` (extracted) | `artex-original` git tree |
| Archive SHA-256 | `93AFBEC089A9D8AB00AE1DA4918DFCAF3A99AF636C1303DAE514F4EBE7CD11E2` | — |
| Archive size | 8,944,269 bytes | — |
| Commit | **unverified** | `e6ec56999175509551a1e43a3ee3b053d0941bd9` (detached HEAD, dirty tree documented) |
| Files / manifest SHA-256 | 794 / `1D7969A0D8487129613233175F03498E79F1840D65F220AEA6042FC86D5748DD` | 2120 / `4BF9796F7F773505FCFC17614021F4F8CF5E6302828C8ACC92348B8EA2EF204D` |
| Go directive | 1.26.3 | 1.26.3 |
| norma | v0.4.4 | v0.4.3 |
| Built binary | `artex-baseline-8332.exe`, 43,968,512 B, exit 0 | `artex-original-embed-v5.exe` |

**Base divergence (critical):** the baseline is the "ARTEX **global edition**" fork (i18n subsystem, `ARTEX_ADMIN_PASSWORD` bootstrap, extra docs/scripts); modified v5 is upstream ARTEX at `e6ec569` plus local `agent/` additions. They are **not the same release ± additions**.

## 3. Runtime evidence

| Service | Port | Health | DB |
|---|---|---|---|
| Baseline | 8888 (proxy 8889) | 200 | `artex_base_pilot` |
| Modified v5 | 8787 (proxy 8788) | 200 | `artex_pilot` |
| Fixture | 3004 | 200 `ok` | stateless |
| PostgreSQL | 5433 | listening | — |

Exact commands:

```
# fixture
python .\bench-vuln.py 3004

# baseline
$env:ARTEX_PG_DSN='postgres://artex@127.0.0.1:5433/artex_base_pilot?sslmode=disable'
$env:ARTEX_ADMIN_PASSWORD='Baseline123!'; $env:ARTEX_LANG='en'
.\artex-baseline-8332.exe -addr 127.0.0.1:8888 -proxy 127.0.0.1:8889 -data .\artex-base-pilot-data

# modified
$env:ARTEX_PG_DSN='postgres://artex@127.0.0.1:5433/artex_pilot?sslmode=disable'
$env:ARTEX_ADMIN_PASSWORD='Pentest123!'; $env:ARTEX_LANG='en'
.\artex-original-embed-v5.exe -addr 127.0.0.1:8787 -proxy 127.0.0.1:8788 -data .\artex-v5-pilot-data
```

Provider config applied identically via authenticated `POST /api/llm` (key from env, redacted):
`provider=openai, model=nvidia/nemotron-3.5-lightning-30b-a3b, base_url=https://integrate.api.nvidia.com/v1, rate 0.3/s & 18/min, context_window_k=128`. Both returned `set=200, get=200, key_set=true`.

## 4. Raw results

| Measurement | Baseline | Modified v5 |
|---|---|---|
| Task | `done` | `done` |
| Runtime | ~200 s | ~160 s |
| LLM calls | 15 | 23 |
| Input / output tokens | 217,838 / 15,009 | 361,005 / 13,624 |
| HTTP requests to fixture | 3 | 4 |
| Endpoints touched | 1 (`/search`) | 2 (`/ok`, `/search`) |
| Findings | 1 | 1 |
| Finding class | `sql_injection` (boolean) | `sql_injection` (boolean) |
| Evidence authentic | yes | yes |
| Out-of-scope traffic | none | none |
| Provider errors | 0 | 0 |
| Non-ok model calls | 0 | 0 |
| Classification | **VALID DETECTION** | **VALID DETECTION** |

## 5. Scoring

Single positive case; TN not measured.

| Metric | Baseline | Modified v5 |
|---|---|---|
| TP / FP / FN | 1 / 0 / 0 | 1 / 0 / 0 |
| Precision | 1.00 (1/1) | 1.00 (1/1) |
| Recall | 1.00 (1/1) | 1.00 (1/1) |
| F1 | 1.00 | 1.00 |

**No measurable detection difference on this case.** Efficiency is mixed: modified faster but more calls/tokens; baseline leaner.

## 6. Limitations

1. **Unverified baseline identity** — no git metadata; cannot claim it is an official upstream release or any specific tag.
2. **Base-version divergence** — the two trees are different upstream revisions, so any difference is not attributable to the v5 additions alone.
3. **Dependency skew** — `norma` v0.4.4 (baseline) vs v0.4.3 (modified).
4. **Single run, single case** — no repetitions, temperature not pinned to 0; model nondeterminism could flip tool selection.
5. **Model identity** — application-level only (no provider request-ID echo).
6. **TN not measured** — the pilot did not task negative-only endpoints.
7. **Oracle advantage untested** — v5's deterministic `Probe` oracle was available but not selected by the planner in this run.
8. **Contamination handling** — both versions were moved to fresh DBs after stale-task discovery; older DBs preserved. This was a deliberate, recorded environment reset, not a source change.
9. **Regression noted** — modified v5 lacks `ARTEX_ADMIN_PASSWORD` headless bootstrap (confirmed live). Due to base divergence, not necessarily introduced by the additions.

## 7. Unresolved questions

- Would repeated runs converge on the same tool choices?
- Would v5 select its deterministic oracles under a different task phrasing (which could change the comparison materially)?
- Do the same conclusions hold for SSTI, XSS, traversal, IDOR, and authenticated SQLi?
- How much of the runtime/token difference is base-version vs additions?

## 8. What this phase did not do

- No detection logic, prompt, tool schema, or fixture behaviour was modified.
- No fallback/mock provider was used.
- No secrets were written to any report or file.
- No superiority verdict is issued.
