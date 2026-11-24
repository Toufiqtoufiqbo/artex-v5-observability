package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/norma/llm"
)

// VIII.4.7 Gate C3 — Norma retry enforcement on the provider path.
//
// Norma retries the establishment phase through the provider's own
// HTTPClient (norma/llm/retry.go: cfg.HTTPClient.Do per attempt). Each
// attempt therefore re-dials through the pinned provider transport: full
// authorization per dial (host, scheme, port, DNS answer set), and
// CheckProviderEgress per request via providerEgressCheckTransport.

func newProviderFixture(t *testing.T, statusFn func(call int32) (int, []byte)) (*httptest.Server, *int32, *int32) {
	t.Helper()
	var hits int32
	var newConns int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&hits, 1)
		code, body := statusFn(call)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(body)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			atomic.AddInt32(&newConns, 1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, &hits, &newConns
}

func buildPinnedOpenAIProvider(t *testing.T, srvURL string, maxRetries int) llm.Provider {
	t.Helper()
	pol := policyForServer(t, srvURL)
	client, err := quotaAwareHTTPClientWithPolicy("", "", srvURL, pol)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	prov, err := llm.NewProvider(llm.Config{
		Format: llm.FormatOpenAI, BaseURL: srvURL, APIKey: "k", Model: "m",
		HTTPClient: client, MaxRetries: maxRetries, RetryInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	return prov
}

func okChatCompletion() (int, []byte) {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{
			"message":      map[string]any{"role": "assistant", "content": "ok"},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{},
	})
	return http.StatusOK, b
}

func TestProviderRetry_Transient500SucceedsViaPinnedClient(t *testing.T) {
	srv, hits, newConns := newProviderFixture(t, func(call int32) (int, []byte) {
		if call == 1 {
			return http.StatusInternalServerError, []byte(`{"error":"boom"}`)
		}
		return okChatCompletion()
	})
	prov := buildPinnedOpenAIProvider(t, srv.URL, 3)
	msg, stop, _, err := prov.Complete(context.Background(), llm.CompletionRequest{Messages: []llm.Message{llm.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_ = stop
	if msg.Text() == "" {
		t.Fatal("expected non-empty completion")
	}
	if got := atomic.LoadInt32(hits); got < 2 {
		t.Fatalf("fixture hits=%d, want >= 2 (transient 500 then success)", got)
	}
	if got := atomic.LoadInt32(newConns); got < 2 {
		t.Fatalf("connections=%d, want >= 2 (every attempt re-dials; DisableKeepAlives prevents reuse)", got)
	}
}

func TestProviderRetry_ContextCancelStopsRetries(t *testing.T) {
	srv, hits, _ := newProviderFixture(t, func(int32) (int, []byte) {
		return http.StatusInternalServerError, []byte(`{"error":"boom"}`)
	})
	prov := buildPinnedOpenAIProvider(t, srv.URL, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for atomic.LoadInt32(hits) == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	_, _, _, err := prov.Complete(ctx, llm.CompletionRequest{Messages: []llm.Message{llm.UserText("hi")}})
	if err == nil {
		t.Fatal("expected error after cancellation")
	}
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(hits); got > 1 {
		t.Fatalf("cancellation must stop retries; fixture hits=%d", got)
	}
}

func TestProviderRetry_PersistentErrorExhaustsBoundedRetries(t *testing.T) {
	srv, hits, _ := newProviderFixture(t, func(int32) (int, []byte) {
		return http.StatusInternalServerError, []byte(`{"error":"boom"}`)
	})
	prov := buildPinnedOpenAIProvider(t, srv.URL, 2)
	start := time.Now()
	_, _, _, err := prov.Complete(context.Background(), llm.CompletionRequest{Messages: []llm.Message{llm.UserText("hi")}})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(hits); got != 3 { // attempt + 2 retries
		t.Fatalf("fixture hits=%d, want 3 (1 attempt + MaxRetries=2)", got)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("retries should be fast with RetryInterval=1ms")
	}
}
