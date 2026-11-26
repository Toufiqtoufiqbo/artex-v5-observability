package agent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderRetryAfterParsing(t *testing.T) {
	header := func(v string) *http.Response {
		return &http.Response{Header: http.Header{"Retry-After": []string{v}}}
	}
	empty := func() *http.Response { return &http.Response{Header: http.Header{}} }

	cases := []struct {
		name string
		resp *http.Response
		body string
		want time.Duration
	}{
		{"seconds header", header("3"), "", 3 * time.Second},
		{"prose hint", empty(), `{"error":{"message":"Please retry in 2.5s"}}`, 2500 * time.Millisecond},
		{"google retryDelay", empty(), `{"retryDelay": "7s"}`, 7 * time.Second},
		{"no hint", empty(), `{"error":{"message":"rate limit reached"}}`, 0},
		{"zero header", header("0"), "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := providerRetryAfter(tc.resp, []byte(tc.body))
			if got != tc.want {
				t.Fatalf("providerRetryAfter = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProviderTransportHonorsRetryAfter(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"rate limit reached"}}`)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	client := &http.Client{Transport: quotaAwareTransport{base: &http.Transport{}}}
	start := time.Now()
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", resp.StatusCode, body)
	}
	if n := atomic.LoadInt32(&hits); n != 2 {
		t.Fatalf("provider hits = %d, want 2 (one 429 + one retry)", n)
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("retry ignored the hint: elapsed %v, want >= ~1s", elapsed)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("retry waited far past the hint: %v", elapsed)
	}
}

func TestProviderTransportCapsRetryAfterWait(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limit reached"}}`)
	}))
	defer srv.Close()

	client := &http.Client{Transport: quotaAwareTransport{base: &http.Transport{}}}
	start := time.Now()
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 when the hint exceeds the cap", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("provider hits = %d, want 1 (no retry past the cap)", n)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("transport blocked for %v despite the cap", elapsed)
	}
}

func TestProviderTransportRetryPreservesPostBody(t *testing.T) {
	var hits int32
	var secondBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"rate limit reached"}}`)
			return
		}
		secondBody = string(b)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	client := &http.Client{Transport: quotaAwareTransport{base: &http.Transport{}}}
	resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{"model":"x"}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if secondBody != `{"model":"x"}` {
		t.Fatalf("retried body = %q, want the original payload", secondBody)
	}
}
