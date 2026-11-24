package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	httpMaxReadBytes   = 2 << 20 // 2 MiB per response
	httpDefaultTimeout = 30 * time.Second
	httpMaxTimeout     = 120 * time.Second
)

// ---------------------------------------------------------------------------
// Session state
//
// Two layers, deliberately separate:
//   - a shared cookie jar: Set-Cookie from any call is replayed automatically on
//     later calls to the same host, so one login authenticates everything after.
//   - a per-host header store: lets an agent pin Authorization / X-API-Key once
//     instead of threading it through every probe.
// Both are process-wide on purpose: a task is one engagement, one identity.
// ---------------------------------------------------------------------------

var (
	jarOnce   sync.Once
	jarShared http.CookieJar

	sessMu       sync.RWMutex
	sessHeaders  = map[string]map[string]string{} // host -> header -> value
)

func sessionJar() http.CookieJar {
	jarOnce.Do(func() {
		jarShared, _ = cookiejar.New(nil)
	})
	return jarShared
}

func sessionKey(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(rawURL)
	}
	return strings.ToLower(u.Host)
}

func setSessionHeaders(host string, h map[string]string) {
	sessMu.Lock()
	defer sessMu.Unlock()
	if h == nil {
		delete(sessHeaders, host)
		return
	}
	sessHeaders[host] = h
}

func getSessionHeaders(host string) map[string]string {
	sessMu.RLock()
	defer sessMu.RUnlock()
	src := sessHeaders[host]
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func allSessionHosts() map[string]map[string]string {
	sessMu.RLock()
	defer sessMu.RUnlock()
	out := make(map[string]map[string]string, len(sessHeaders))
	for k, v := range sessHeaders {
		cp := make(map[string]string, len(v))
		for hk, hv := range v {
			cp[hk] = hv
		}
		out[k] = cp
	}
	return out
}

// ---------------------------------------------------------------------------
// HTTP client
// ---------------------------------------------------------------------------

type httpCall struct {
	Method          string
	URL             string
	Headers         map[string]string
	Body            string
	ContentType     string
	Timeout         time.Duration
	Insecure        bool
	FollowRedirects bool
	NoJar           bool
}

type httpResult struct {
	Status    int
	Proto     string
	Header    http.Header
	Body      []byte
	Elapsed   time.Duration
	Truncated bool
}

// doHTTP is the single transport used by HTTPRequest, the differential oracles
// and the crawler. Centralising it is what makes the shared session apply
// everywhere instead of only to tools that remembered to wire a jar.
// dialFn is the process dial function used by the transport (VIII.2.2 network
// boundary accounting). Tests replace it to count attempted connections —
// the dial layer is the layer where "no unauthorized connection" must hold.
var dialFn = defaultDialContext

func defaultDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, addr)
}

// resolvePinnedIP resolves host and returns the first address that is
// address-authorized (VIII.4.5). Resolver errors and empty answers are
// failures; unauthorizable answers are ErrScopeDenied.
// selectAuthorizedIP applies the request-scoped DNS policy (VIII.4.5.5):
// an empty answer, any prohibited answer, or a resolver error fails closed;
// only when every answer is authorized is the first one selected. There is no
// filtering or fallback. No global state is used.
func selectAuthorizedIP(key, host string, addrs []net.IPAddr) (net.IP, error) {
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: empty DNS answer for %q", ErrScopeDenied, host)
	}
	var selected net.IP
	for _, a := range addrs {
		if !addressAuthorized(key, a.IP, host) {
			return nil, fmt.Errorf("%w: DNS answer %s for %q is not authorized", ErrScopeDenied, a.IP, host)
		}
		if selected == nil {
			selected = a.IP
		}
	}
	return selected, nil
}

func resolvePinnedIP(ctx context.Context, key, host string) (net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%w: DNS resolution failed for %q: %v", ErrScopeDenied, host, err)
	}
	return selectAuthorizedIP(key, host, addrs)
}

// dialPinnedContext validates the destination and dials the validated IP
// (VIII.4.5). The caller's URL host is preserved for Host/TLS; the actual TCP
// connection goes to the validated address.
func dialPinnedContext(ctx context.Context, network, addr string) (net.Conn, error) {
	key := scopeKeyFromContext(ctx)
	host, port := splitHostPort(addr)
	if ip := net.ParseIP(host); ip != nil {
		if !addressAuthorized(key, ip, host) {
			return nil, fmt.Errorf("%w: literal destination %q is not authorized", ErrScopeDenied, addr)
		}
		return dialFn(ctx, network, addr)
	}
	ip, err := resolvePinnedIP(ctx, key, host)
	if err != nil {
		return nil, err
	}
	return dialFn(ctx, network, net.JoinHostPort(ip.String(), port))
}

func splitHostPort(addr string) (string, string) {
	if h, p, err := net.SplitHostPort(addr); err == nil {
		return h, p
	}
	return addr, ""
}

func doHTTP(ctx context.Context, c httpCall) (*httpResult, error) {
	method := strings.ToUpper(strings.TrimSpace(c.Method))
	if method == "" {
		method = "GET"
	}
	rawURL := strings.TrimSpace(c.URL)
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid url %q (need absolute http(s):// URL)", rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q (only http/https)", u.Scheme)
	}
	// VIII.4.3 — Hard floor (Phase 7): no request is sent if the host is out of scope.
	if err := scopeGuard(ctx, rawURL); err != nil {
		emitEgressEvent(EgressEventForURL(EgressTargetAssessment, int64(RunInfoFrom(ctx).TaskID), rawURL, EgressDecisionDeny, err.Error()))
		return nil, err
	}
	emitEgressEvent(EgressEventForURL(EgressTargetAssessment, int64(RunInfoFrom(ctx).TaskID), rawURL, EgressDecisionAllow, "scope-checked"))

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = httpDefaultTimeout
	}
	if timeout > httpMaxTimeout {
		timeout = httpMaxTimeout
	}

	var bodyReader io.Reader
	if c.Body != "" {
		bodyReader = strings.NewReader(c.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("User-Agent", "artex/0.3.15 (+authorized-assessment)")
	req.Header.Set("Accept", "*/*")
	if c.Body != "" && c.ContentType == "" {
		if looksLikeJSON(c.Body) {
			req.Header.Set("Content-Type", "application/json")
		} else {
			req.Header.Set("Content-Type", "text/plain; charset=utf-8")
		}
	}
	if c.ContentType != "" {
		req.Header.Set("Content-Type", c.ContentType)
	}

	// Session defaults first (per host), then explicit call headers override them.
	for k, v := range getSessionHeaders(sessionKey(rawURL)) {
		if k == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	for k, v := range c.Headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	tr := &http.Transport{
		// VIII.4.3 (Phase B): environment proxies do not authorize target
		// destinations; the logical destination passed scopeGuard above.
		// doHTTP clients go direct unless an explicit proxy policy is added.
		Proxy:               egressHTTPProxyPolicy,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: c.Insecure},
		TLSHandshakeTimeout: 15 * time.Second,
		// VIII.4.5.4 — no connection reuse: every request must pass through
		// dialPinnedContext so the destination is re-authorized on every dial.
		DisableKeepAlives: true,
		MaxIdleConns:      0,
		// VIII.4.5.5: no HTTP/2 multiplexing on scoped target requests.
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
		// VIII.4.5 — the actual dial is bound to a validated IP.
		DialContext: dialPinnedContext,
	}
	client := &http.Client{Transport: tr, Timeout: timeout}
	if !c.NoJar {
		client.Jar = sessionJar()
	}
	if !c.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		// VIII.2.1 P0: revalidate EVERY redirect destination. Authorization
		// is never inherited across origins, and credentials are stripped
		// before following a cross-origin hop.
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("redirect limit exceeded (10)")
			}
			if err := scopeGuard(ctx, req.URL.String()); err != nil {
				emitEgressEvent(EgressEventForURL(EgressTargetAssessment, int64(RunInfoFrom(ctx).TaskID), req.URL.String(), EgressDecisionDeny, err.Error()))
				return err
			}
			if len(via) > 0 {
				prev := via[len(via)-1]
				if !strings.EqualFold(prev.URL.Hostname(), req.URL.Hostname()) {
					req.Header.Del("Authorization")
					req.Header.Del("Cookie")
				}
			}
			return nil
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("%s %s failed after %s: %w", method, rawURL, elapsed.Round(time.Millisecond), err)
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, httpMaxReadBytes))
	return &httpResult{
		Status:    resp.StatusCode,
		Proto:     fmt.Sprintf("HTTP/%d.%d", resp.ProtoMajor, resp.ProtoMinor),
		Header:    resp.Header,
		Body:      raw,
		Elapsed:   elapsed,
		Truncated: readErr != nil,
	}, nil
}

func looksLikeJSON(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

// injectParam returns rawURL with param=value applied to the query string.
func injectQuery(rawURL, param, value string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set(param, value)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// injectForm returns an application/x-www-form-urlencoded body with the
// parameter set, preserving any extra pairs.
func injectForm(extra map[string]string, param, value string) string {
	v := url.Values{}
	for k, val := range extra {
		v.Set(k, val)
	}
	v.Set(param, value)
	return v.Encode()
}

// injectJSON returns a JSON object body with the parameter set, preserving any
// scalar extra fields.
func injectJSON(extra map[string]string, param, value string) string {
	obj := map[string]any{}
	for k, v := range extra {
		obj[k] = v
	}
	obj[param] = value
	b, _ := json.Marshal(obj)
	return string(b)
}
