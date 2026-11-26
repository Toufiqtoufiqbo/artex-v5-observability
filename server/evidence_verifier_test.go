package server

import (
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
