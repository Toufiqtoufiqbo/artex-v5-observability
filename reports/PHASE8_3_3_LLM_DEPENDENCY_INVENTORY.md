# PHASE8_3_3 — LLM DEPENDENCY INVENTORY

Environment check (executed): baseline 8888 pid 11080 health 200; modified 8787 pid 12736 health 200; fixture 3004 pid 1120 → `200 ok`; PG 5433 pid 3684. Both instances isolated (DBs `artex_base` / `artex`, separate ports/data/logs).

Code-inspected execution path (both versions): `cmd/artex` starts an HTTP server only; detection tools (`Probe`, `HTTPRequest`, `Crawl`, `Login`, `IdorDiff`, `Fuzz` in v5; WebFetch/Bash in baseline) are exposed to the **planner/worker LLM** via `agent/worker.go` toolsets. No standalone CLI/API entry point invokes an oracle without model inference.

| Task | Entry point | LLM call? | Class |
|---|---|---|---|
| SQLi / SSTI / XSS / traversal / IDOR detection | planner intent → worker tool call | YES | LLM_REQUIRED |
| Crawl / endpoint discovery | worker tool call | YES | LLM_REQUIRED |
| Login / session flow | worker tool call | YES | LLM_REQUIRED |
| Health / auth (harness checks) | direct HTTP to ARTEX API | NO | DETERMINISTIC (infrastructure only, not a detection task) |

Conclusion: the **common deterministic detection subset is empty** — no detection capability in either version can be exercised without model inference. v5's oracles compute verdicts in Go once invoked, but invocation itself is LLM-driven. Baseline's detection is entirely LLM/Bash/WebFetch-driven.

No mock provider was substituted; no model call was skipped and reported as success.
