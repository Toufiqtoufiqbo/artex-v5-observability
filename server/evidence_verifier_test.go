package server

import (
	"context"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func TestClaimMatchesExchange(t *testing.T) {
	cases := []struct {
		name       string
		claim, req string
		resp       string
		want       bool
	}{
		{
			name:  "real path and status",
			claim: "POST /rest/user/login -> HTTP/1.1 401",
			req:   "POST /rest/user/login HTTP/1.1\r\nHost: 127.0.0.1:3000",
			resp:  "HTTP/1.1 401 Unauthorized\r\nContent-Length: 12",
			want:  true,
		},
		{
			name:  "invented path with real status",
			claim: "POST /admin/secret -> HTTP/1.1 401",
			req:   "POST /rest/user/login HTTP/1.1",
			resp:  "HTTP/1.1 401 Unauthorized",
			want:  false,
		},
		{
			name:  "real path with invented status",
			claim: "GET /rest/user/login -> HTTP/1.1 200",
			req:   "GET /rest/user/login HTTP/1.1",
			resp:  "HTTP/1.1 401 Unauthorized",
			want:  false,
		},
		{
			name:  "distinctive output token",
			claim: "response contained token=bWluZWRhdGE6NDI=",
			req:   "GET /api/profile HTTP/1.1",
			resp:  "HTTP/1.1 200 OK\r\n{\"token\":\"bWluZWRhdGE6NDI=\"}",
			want:  true,
		},
		{
			name:  "measured negative status only",
			claim: "port 3000 refused HTTP/1.1 404",
			req:   "GET /missing HTTP/1.1",
			resp:  "HTTP/1.1 404 Not Found",
			want:  true,
		},
		{
			name:  "status only in body is not the response status",
			claim: "GET /api/profile -> HTTP/1.1 200",
			req:   "GET /api/profile HTTP/1.1\r\nHost: fixture.local",
			resp:  "HTTP/1.1 401 Unauthorized\r\nbody: HTTP/1.1 200 OK",
			want:  false,
		},
		{
			name:  "path from a different request fails",
			claim: "GET /admin/config -> HTTP/1.1 401",
			req:   "GET /api/profile HTTP/1.1",
			resp:  "HTTP/1.1 401 Unauthorized",
			want:  false,
		},
		{
			name:  "real request line and status line pass",
			claim: "POST /rest/user/login returned HTTP/1.1 401",
			req:   "POST /rest/user/login?next=1 HTTP/1.1\r\nHost: fixture.local",
			resp:  "HTTP/1.1 401 Unauthorized\r\n{\"error\":\"denied\"}",
			want:  true,
		},
		{
			name:  "body token cannot rescue a wrong path",
			claim: "GET /admin/config -> HTTP/1.1 401 token=bWluZWRhdGE6NDI=",
			req:   "GET /api/profile HTTP/1.1",
			resp:  "HTTP/1.1 401 Unauthorized\r\n{\"token\":\"bWluZWRhdGE6NDI=\"}",
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimMatchesExchange(tc.claim, tc.req, tc.resp); got != tc.want {
				t.Fatalf("claimMatchesExchange=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestFactTrafficIDs(t *testing.T) {
	n := &db.Node{Payload: []byte(`{"summary":"probe","traffic_refs":[{"traffic_id":"abc","role":"proof"},{"traffic_id":"abc"}],"traffic_ref":"legacy-id"}`)}
	got := factTrafficIDs(n)
	want := []string{"abc", "legacy-id"}
	if len(got) != len(want) {
		t.Fatalf("ids=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids=%v, want %v", got, want)
		}
	}
	if got := factTrafficIDs(&db.Node{Payload: []byte(`{"summary":"no traffic"}`)}); len(got) != 0 {
		t.Fatalf("unexpected ids: %v", got)
	}
}

func TestFactAnchors(t *testing.T) {
	got := factAnchors("GET /rest/user/login returned HTTP/1.1 401 with 12345 bytes")
	joined := strings.Join(got, "|")
	for _, want := range []string{"/rest/user/login", "HTTP/1.1 401", "12345 bytes"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("anchors %q missing %q", joined, want)
		}
	}
}

func TestGoalEvidenceVerifierFallsBackToActivityLedger(t *testing.T) {
	// Traffic capture disabled (or never started) must not turn goal proof back
	// into a shape check: the ledger verifier is still wired in.
	s := &Server{m: &Manager{pg: &db.DB{}}}
	v := s.goalEvidenceVerifier()
	if v == nil {
		t.Fatal("verifier is nil despite an available activity ledger")
	}
	tv, ok := v.(*trafficEvidenceVerifier)
	if !ok {
		t.Fatalf("unexpected verifier type %T", v)
	}
	if tv.traffic != nil {
		t.Fatal("traffic backend was injected while capture is disabled")
	}
}

func TestGoalEvidenceVerifierWithoutLedgerIsNil(t *testing.T) {
	if v := (&Server{m: &Manager{}}).goalEvidenceVerifier(); v != nil {
		t.Fatalf("no backend at all should leave no verifier, got %T", v)
	}
	if v := (&Server{}).goalEvidenceVerifier(); v != nil {
		t.Fatalf("server without manager should leave no verifier, got %T", v)
	}
}

func TestVerifyFactCitedTrafficWithoutBackendFailsClosed(t *testing.T) {
	v := &trafficEvidenceVerifier{pg: &db.DB{}}
	node := &db.Node{Kind: db.KindFact, Payload: []byte(`{"summary":"login","detail":"GET /login returned HTTP/1.1 200","traffic_refs":[{"traffic_id":"1760000000-0001","role":"proof"}]}`)}
	err := v.VerifyFact(context.Background(), 1, node)
	if err == nil || !strings.Contains(err.Error(), "no recording backend") {
		t.Fatalf("cited traffic was not rejected without a backend: %v", err)
	}
}

func TestVerifyFactNeedsTwoAnchorsBeforeLedger(t *testing.T) {
	v := &trafficEvidenceVerifier{}
	node := &db.Node{Kind: db.KindFact, Payload: []byte(`{"summary":"probe","detail":"GET /only-a-path"}`)}
	err := v.VerifyFact(context.Background(), 1, node)
	if err == nil || !strings.Contains(err.Error(), "at least two") {
		t.Fatalf("single anchor was not rejected: %v", err)
	}
}
