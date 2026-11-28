package evidence

import (
	"context"
	"testing"
	"time"
)

func seedAutobindExchange(t *testing.T, s *Store, id string, status int) {
	t.Helper()
	_, err := s.Traffic.DB().Exec(`INSERT INTO exchanges(id,ts,host,method,url_template,url,status,content_type,req_len,resp_len,path) VALUES(?,?,'127.0.0.1','GET','/api/products',?,?,?,0,64,'')`,
		id, time.Now().Unix(), "http://127.0.0.1:3000/api/products", status, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Traffic.DB().Exec(`INSERT INTO exchange_bodies(id,req_head,req_body,resp_head,resp_body) VALUES(?,?,?,?,?)`,
		id, "GET /api/products HTTP/1.1\nHost: 127.0.0.1:3000\n", []byte(""), "HTTP 401\n", []byte(`{"error":"UnauthorizedError: No Authorization header was found"}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestAutoBindFromEvidenceExactMatch(t *testing.T) {
	store, in, _ := evidenceFixture(t)
	store.AutoBind = true
	seedAutobindExchange(t, store, "autobind-exact-1", 401)
	in.Name = t.Name()
	in.Evidence = "HTTP 401 response: 'UnauthorizedError: No Authorization header was found' from GET /api/products"

	got, err := store.Record(context.Background(), in, nil)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(got.Traffic.Bindings) != 1 {
		t.Fatalf("want 1 auto-bound exchange, got %d", len(got.Traffic.Bindings))
	}
	if got.Traffic.Bindings[0].Snapshot.SourceTrafficID != "autobind-exact-1" {
		t.Fatalf("bound wrong exchange: %s", got.Traffic.Bindings[0].Snapshot.SourceTrafficID)
	}
}

func TestAutoBindSkippedWhenDisabledOrAmbiguous(t *testing.T) {
	store, in, _ := evidenceFixture(t)
	seedAutobindExchange(t, store, "autobind-exact-2", 401)

	// Switch off: no refs may be invented from free text.
	in.Name = t.Name() + "-disabled"
	in.Evidence = "HTTP 401 response from GET /api/products"
	got, err := store.Record(context.Background(), in, nil)
	if err != nil {
		t.Fatalf("Record(disabled): %v", err)
	}
	if len(got.Traffic.Bindings) != 0 {
		t.Fatalf("AutoBind off must not bind, got %d", len(got.Traffic.Bindings))
	}

	// Switch on but no status code in the evidence: never guess by path alone.
	store.AutoBind = true
	in.Name = t.Name() + "-ambiguous"
	in.Evidence = "GET /api/products returned an error"
	got, err = store.Record(context.Background(), in, nil)
	if err != nil {
		t.Fatalf("Record(ambiguous): %v", err)
	}
	if len(got.Traffic.Bindings) != 0 {
		t.Fatalf("ambiguous evidence must not bind, got %d", len(got.Traffic.Bindings))
	}
}

func TestProbesFromEvidenceStatusBeforeRequest(t *testing.T) {
	// Real reporter wording: "HTTP 200 response from POST /path ... baseline
	// returns HTTP 401". The nearest status is 200, not the later baseline 401.
	text := "HTTP 200 response from POST http://127.0.0.1:3000/rest/user/login with email payload; wrong credentials return HTTP 401"
	probes := probesFromEvidence(text)
	if len(probes) != 1 || probes[0].Status != "200" || probes[0].Path != "/rest/user/login" {
		t.Fatalf("wrong probe: %+v", probes)
	}
}

func TestProbesFromEvidence(t *testing.T) {
	probes := probesFromEvidence("Baseline: GET /api/products -> HTTP 200\nInjection: POST /login -> status=401")
	if len(probes) != 2 {
		t.Fatalf("want 2 probes, got %+v", probes)
	}
	if probes[0].Method != "GET" || probes[0].Path != "/api/products" || probes[0].Status != "200" {
		t.Fatalf("bad first probe: %+v", probes[0])
	}
	if probes[1].Method != "POST" || probes[1].Status != "401" {
		t.Fatalf("bad second probe: %+v", probes[1])
	}
	if got := probesFromEvidence("vulnerability looks likely but nothing was triggered"); got != nil {
		t.Fatalf("suspicion-only text produced probes: %+v", got)
	}
}
