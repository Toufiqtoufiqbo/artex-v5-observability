# PHASE8_2_1 — company scope semantics

Code: `agent/scopecheck.go:86-89` — `case "company": continue` (VIII.2 fix). Company is an **organizational label, not an authorization rule**: no host-to-company verification exists in `CheckScope`, so company-only scope now DENIES all hosts (fail closed). Explicit host authorization requires `root_domain`/`subdomain`/`ip`/`cidr` entries. `TestCheckScope_CompanyFailsClosed` verifies: company label only → DENY.

Matrix: authorized host ALLOW; unauthorized DENY; company label only DENY; company+verified asset mapping → NOT SUPPORTED → DENY (fail closed); empty scope → currently ALLOW (unverified/unchanged — explicit requirement not met); malformed → DENY via parse error; ordering → no match-dependent behavior.

Limitation: "empty scope default deny" NOT implemented (would break unscoped agents); company→asset mapping unsupported.
