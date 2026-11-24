package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// VIII.4.3 Phase I — doHTTP emits structured, secret-safe egress events.

func TestEgressEventsEmitOnAllowAndDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ClearTaskScopeForTask(901)
	defer ClearTaskScopeForTask(901)
	SetTaskScopeForTask(901, []db.TaskScope{{Kind: "ip", Net: hostOf(srv.URL) + "/32"}})

	var mu sync.Mutex
	var events []EgressEvent
	SetEgressAuditSink(func(ev EgressEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})
	defer SetEgressAuditSink(nil)

	ctx := WithRunInfo(context.Background(), RunInfo{TaskID: 901})
	if _, err := doHTTP(ctx, httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("allowed doHTTP: %v", err)
	}
	if _, err := doHTTP(ctx, httpCall{Method: "GET", URL: "http://10.255.255.1/", NoJar: true, Timeout: 5 * time.Second}); err == nil {
		t.Fatal("expected deny for out-of-scope destination")
	}

	mu.Lock()
	defer mu.Unlock()
	var sawAllow, sawDeny bool
	for _, ev := range events {
		if ev.Category != EgressTargetAssessment {
			t.Fatalf("event category: %v", ev.Category)
		}
		switch ev.Decision {
		case EgressDecisionAllow:
			sawAllow = true
			if ev.Host == "" {
				t.Errorf("allow event missing host: %+v", ev)
			}
		case EgressDecisionDeny:
			sawDeny = true
			if ev.Reason == "" {
				t.Errorf("deny event missing reason: %+v", ev)
			}
		}
		// Secret-safety: no key-like material in any event.
		if strings.Contains(ev.Host+ev.Reason+ev.Scheme+ev.Port, "nvapi-") {
			t.Errorf("secret-like string in event: %+v", ev)
		}
	}
	if !sawAllow {
		t.Errorf("expected allow event, got: %+v", events)
	}
	if !sawDeny {
		t.Errorf("expected deny event, got: %+v", events)
	}
}
