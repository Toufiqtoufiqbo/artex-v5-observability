package db

import (
	"context"
	"strings"
	"testing"
)

// The two reports below are the real shape of the duplicate seen in a local
// Juice Shop assessment: same endpoint and injection field, different wording.
// v1 hashed the literal title and kept both rows.
const (
	v2LoginEvidenceA = `POST /rest/user/login with JSON body {"email":"' OR 1=1--","password":"pass"}

Proof:
POST /rest/user/login returns HTTP 200 with an admin JWT token when using SQL injection payload in email field.`
	v2LoginEvidenceB = `POST /rest/user/login
Host: 127.0.0.1:3000
Content-Type: application/json

{"email":"' OR 1=1 --","password":"password"}

Response: HTTP/1.1 200 OK
{"authentication":{"token":"...","bid":1,"umail":"admin@juice-sh.op"}}`
)

func TestFindingIdentityV2DedupsRenamedSameIssue(t *testing.T) {
	const task int64 = 42
	a := FindingFingerprintV2(task, "sql_injection",
		"Authentication Bypass via SQL Injection in Login",
		"SQL injection in the login form allows authentication bypass as admin user.",
		v2LoginEvidenceA, nil)
	b := FindingFingerprintV2(task, "sql_injection",
		"SQL Injection Authentication Bypass on Login Endpoint",
		"SQL injection vulnerability in the login endpoint (/rest/user/login) allows complete authentication bypass and admin account takeover.",
		v2LoginEvidenceB, nil)
	if a != b {
		t.Fatalf("same endpoint/param reported with different titles was not merged:\n %s\n %s", a, b)
	}
	if !strings.HasPrefix(a, FindingIdentityVersionV2+":") {
		t.Fatalf("missing v2 prefix: %s", a)
	}
	if len(strings.TrimPrefix(a, FindingIdentityVersionV2+":")) != 64 {
		t.Fatalf("fingerprint payload is not sha256 hex: %s", a)
	}
	// The old identity is deliberately left untouched (no silent rewrite).
	v1a := FindingFingerprint(task, "sql_injection", "Authentication Bypass via SQL Injection in Login", nil)
	v1b := FindingFingerprint(task, "sql_injection", "SQL Injection Authentication Bypass on Login Endpoint", nil)
	if v1a == v1b || a == v1a {
		t.Fatal("v1/v2 identity versions collided")
	}
}

func TestExtractFindingResource(t *testing.T) {
	r := ExtractFindingResource(
		"SQL Injection in Product Search Search Query Parameter",
		"SQL injection vulnerability in the search product query parameter allowing full table data retrieval.",
		"GET /rest/products/search?q=%27%27%29%20OR%201=1-- returns all products")
	if r.Method != "GET" {
		t.Errorf("method = %q, want GET", r.Method)
	}
	if r.Endpoint != "/rest/products/search?q" {
		t.Errorf("endpoint = %q, want /rest/products/search?q (query VALUES must be dropped)", r.Endpoint)
	}
	if r.Params != "q" {
		t.Errorf("params = %q, want q (prose stopwords must not become params)", r.Params)
	}
	if r.Fallback != "" {
		t.Errorf("fallback = %q, want empty when a resource was found", r.Fallback)
	}

	login := ExtractFindingResource("Login bypass", "login endpoint", v2LoginEvidenceA)
	if login.Method != "POST" || login.Endpoint != "/rest/user/login" || login.Params != "email" {
		t.Errorf("login resource = %+v, want POST /rest/user/login email", login)
	}
}

func TestFindingIdentityV2KeepsDistinctResourcesApart(t *testing.T) {
	const task int64 = 7
	fp := func(vuln, name, summary, evidence string) string {
		return FindingFingerprintV2(task, vuln, name, summary, evidence, []int64{3})
	}
	base := fp("sql_injection", "SQLi in login", "login", v2LoginEvidenceA)
	cases := []struct {
		name, vuln, summary, evidence string
	}{
		{"different endpoint", "sql_injection", "search", "GET /rest/products/search?q=' returns all products"},
		{"different parameter", "sql_injection", "password field injection", "POST /rest/user/login body {\"password\":\"' OR 1=1--\"}"},
		{"different method", "sql_injection", "login", "PUT /rest/user/login body {\"email\":\"' OR 1=1--\"}"},
		{"different vulnclass", "xss", "login", v2LoginEvidenceA},
		{"different asset", "sql_injection", "login", v2LoginEvidenceA},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assets := []int64{3}
			if tc.name == "different asset" {
				assets = []int64{4}
			}
			got := FindingFingerprintV2(task, tc.vuln, tc.name, tc.summary, tc.evidence, assets)
			if got == base {
				t.Fatalf("distinct finding merged into %s", base)
			}
		})
	}

	// Two genuinely different query parameters on one endpoint must stay apart
	// even though the titles are nearly identical.
	id := fp("sqli", "SQLi (param id)", "s", "GET /api/items?id=' OR 1=1--")
	sortParam := fp("sqli", "SQLi (param sort)", "s", "GET /api/items?sort=' OR 1=1--")
	if id == sortParam {
		t.Fatal("different injection parameters on the same endpoint were merged")
	}
	// A wire parameter literally named like a prose stopword is still identity.
	queryParam := fp("sqli", "SQLi A", "search query parameter allowing data", "GET /api/items?query=' OR 1=1--")
	searchParam := fp("sqli", "SQLi B", "search query parameter allowing data", "GET /api/items?search=' OR 1=1--")
	if queryParam == searchParam {
		t.Fatal("real parameters named query/search were dropped as prose stopwords")
	}
	// Numeric ids are endpoint shape, not identity; query key order is noise.
	a := fp("idor", "users 5", "s", "GET /api/users/5?b=1&a=2")
	b := fp("idor", "users 9", "s", "GET /api/users/9?a=2&b=1")
	if a != b {
		t.Fatal("opaque ids / query key order changed the identity")
	}
	// No evidence at all: the title remains a safe last-resort discriminator.
	if fp("sqli", "SQLi (boolean)", "s", "poc") == fp("sqli", "SQLi (time)", "s", "poc") {
		t.Fatal("titleless-evidence findings with different titles were merged")
	}
}

// Real persistence path: two differently worded reports of the same issue must
// reuse one row. Uses the disposable ARTEX_PG_DSN database, never a user DB.
func TestFindingDedupRenamedSameEndpoint_v2(t *testing.T) {
	_, task, es, marker := newDedupFixture(t, "renamed-endpoint")
	ctx := context.Background()
	first := RecordFindingInput{TaskID: task.ID, VulnClass: marker,
		Name: "Authentication Bypass via SQL Injection in Login", Severity: "critical",
		Summary:  "SQL injection in the login form allows authentication bypass as admin user.",
		Evidence: v2LoginEvidenceA, Worker: "w"}
	second := RecordFindingInput{TaskID: task.ID, VulnClass: marker,
		Name: "SQL Injection Authentication Bypass on Login Endpoint", Severity: "critical",
		Summary:  "SQL injection vulnerability in the login endpoint (/rest/user/login) allows complete authentication bypass.",
		Evidence: v2LoginEvidenceB, Worker: "w"}
	r1, err := es.RecordFinding(ctx, first)
	if err != nil || r1 == nil || r1.Outcome != "new" {
		t.Fatalf("first: %+v err=%v", r1, err)
	}
	r2, err := es.RecordFinding(ctx, second)
	if err != nil || r2 == nil {
		t.Fatalf("second: %+v err=%v", r2, err)
	}
	if r2.Outcome != "duplicate" || r2.FindingID != r1.FindingID {
		t.Fatalf("renamed same-issue report created a second finding: first=%+v second=%+v", r1, r2)
	}
}
