package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

const httpToolDefaultChars = 12000

// newHTTPTool builds HTTPRequest: a first-class HTTP client for the agent.
// Why this exists (observed in the field, not theoretical): workers had no HTTP
// primitive, so they shelled out to `curl` through Bash. On Windows `curl`
// resolves to Invoke-WebRequest, which rejects -X / -H / --data and emits
// "Cannot bind parameter 'Headers'", so every POST, JSON-RPC, MCP, GraphQL and
// form-login probe failed while GET probes through WebFetch still worked. That
// skewed every assessment toward read-only surface. A native client removes the
// shell (and its quoting rules) from the path entirely.
func newHTTPTool() actool.CoreTool {
	schema := obj(map[string]any{
		"method": map[string]any{
			"type":        "string",
			"enum":        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
			"description": "HTTP method (default GET).",
		},
		"url": str("Absolute http(s) URL, including scheme."),
		"headers": map[string]any{
			"type":                 "object",
			"description":          "Request headers as name/value pairs. Override the pinned session headers for this call.",
			"additionalProperties": map[string]any{"type": "string"},
		},
		"body":         str("Raw request body. For JSON, send the JSON text here and set content_type. Ignored when form_fields/files are provided."),
		"body_base64":  str("Optional raw body encoded as base64. Use when body is binary. Ignored when form_fields/files are provided."),
		"form_fields":  map[string]any{"type": "object", "description": "Multipart form fields as name/value pairs. When set, request is sent as multipart/form-data.", "additionalProperties": map[string]any{"type": "string"}},
		"files": map[string]any{
			"type":        "array",
			"description": "Multipart file parts. When set, request is sent as multipart/form-data.",
			"items": obj(map[string]any{
				"field":        str("Form field name, e.g. uploadedFile."),
				"filename":     str("Filename to send, e.g. ../test.txt or shell.jsp."),
				"content":      str("File content as text."),
				"content_base64": str("Optional file content encoded as base64."),
				"content_type": str("Optional part Content-Type."),
			}, "field"),
		},
		"content_type":     str("Content-Type header. Defaults to application/json when the body parses as JSON, otherwise text/plain. For multipart, boundary is set automatically unless you explicitly provide a full multipart content-type."),
		"timeout_ms":       intp("Timeout in milliseconds (default 30000, max 120000)."),
		"insecure":         map[string]any{"type": "boolean", "description": "Skip TLS certificate verification (self-signed lab targets)."},
		"follow_redirects": map[string]any{"type": "boolean", "description": "Follow 3xx redirects. Default false so the status and Location header stay visible."},
		"max_body_chars":   intp("Maximum response-body characters returned (default 12000)."),
	}, "url")

	desc := "Send a raw HTTP request (any method, custom headers, raw body, or multipart form/file upload) and return the status line, response headers and body. Use this for POST/PUT/PATCH/DELETE, JSON APIs, form logins, JSON-RPC/MCP, GraphQL, file uploads, and any probe where the method or body matters. For multipart uploads, provide form_fields and/or files; do not manually set a multipart boundary. Cookies are shared per host across calls, so one successful login authenticates later requests automatically."

	return actool.Build(actool.Spec{
		Name:        "HTTPRequest",
		Description: desc,
		Prompt: "HTTPRequest speaks HTTP directly: no shell, no quoting rules. Set method/url/headers/body and read the returned status, headers and body. Redirects are not followed by default (so a 302 and its Location are visible); set follow_redirects to change that. Authenticated probes: cookies set by a login response are replayed automatically to the same host, and headers pinned with the Session tool are applied automatically. Every call is recorded.",
		Schema:      schema,
		ReadOnly: func(in json.RawMessage) bool {
			return isSafeMethod(methodOf(in))
		},
		Concurrent: func(in json.RawMessage) bool {
			return isSafeMethod(methodOf(in))
		},
		Permissions: func(context.Context, json.RawMessage, acperm.Context) acperm.Decision {
			return acperm.Allowed()
		},
		Run: runHTTPRequest,
	})
}

func methodOf(in json.RawMessage) string {
	var a struct {
		Method string `json:"method"`
	}
	_ = json.Unmarshal(in, &a)
	m := strings.ToUpper(strings.TrimSpace(a.Method))
	if m == "" {
		return "GET"
	}
	return m
}

func isSafeMethod(m string) bool {
	switch m {
	case "GET", "HEAD", "OPTIONS":
		return true
	}
	return false
}

func runHTTPRequest(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	var a struct {
		Method          string            `json:"method"`
		URL             string            `json:"url"`
		Headers         map[string]string `json:"headers"`
		Body            string            `json:"body"`
		BodyBase64      string            `json:"body_base64"`
		FormFields      map[string]string `json:"form_fields"`
		Files           []struct {
			Field         string `json:"field"`
			Filename      string `json:"filename"`
			Content       string `json:"content"`
			ContentBase64 string `json:"content_base64"`
			ContentType   string `json:"content_type"`
		} `json:"files"`
		ContentType     string `json:"content_type"`
		TimeoutMS       int    `json:"timeout_ms"`
		Insecure        bool   `json:"insecure"`
		FollowRedirects bool   `json:"follow_redirects"`
		MaxBodyChars    int    `json:"max_body_chars"`
	}
	if err := json.Unmarshal(in, &a); err != nil {
		return actool.Errorf("HTTPRequest: invalid input: " + err.Error()), nil
	}

	method := strings.ToUpper(strings.TrimSpace(a.Method))
	if method == "" {
		method = "GET"
	}
	rawURL := strings.TrimSpace(a.URL)

	timeout := httpDefaultTimeout
	if a.TimeoutMS > 0 {
		timeout = time.Duration(a.TimeoutMS) * time.Millisecond
	}

	call := httpCall{
		Method:          method,
		URL:             rawURL,
		Headers:         a.Headers,
		ContentType:     a.ContentType,
		Timeout:         timeout,
		Insecure:        a.Insecure,
		FollowRedirects: a.FollowRedirects,
	}

	hasMultipart := len(a.FormFields) > 0 || len(a.Files) > 0
	if hasMultipart {
		call.MultipartFields = a.FormFields
		for _, f := range a.Files {
			field := strings.TrimSpace(f.Field)
			if field == "" {
				return actool.Errorf("HTTPRequest: multipart file field is required"), nil
			}
			content := []byte(f.Content)
			if f.ContentBase64 != "" {
				decoded, err := base64.StdEncoding.DecodeString(f.ContentBase64)
				if err != nil {
					decoded, err = base64.RawStdEncoding.DecodeString(f.ContentBase64)
				}
				if err != nil {
					return actool.Errorf(fmt.Sprintf("HTTPRequest: invalid content_base64 for field %q: %v", field, err)), nil
				}
				content = decoded
			}
			call.MultipartFiles = append(call.MultipartFiles, httpMultipartFile{
				Field:       field,
				Filename:    f.Filename,
				Content:     content,
				ContentType: f.ContentType,
			})
		}
	} else if a.BodyBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(a.BodyBase64)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(a.BodyBase64)
		}
		if err != nil {
			return actool.Errorf("HTTPRequest: invalid body_base64: " + err.Error()), nil
		}
		call.Body = string(decoded)
	} else {
		call.Body = a.Body
	}

	res, err := doHTTP(ctx, call)
	if err != nil {
		return actool.Errorf("HTTPRequest: " + err.Error()), nil
	}

	maxChars := a.MaxBodyChars
	if maxChars <= 0 {
		maxChars = httpToolDefaultChars
	}
	body := string(res.Body)
	truncated := res.Truncated
	if len(body) > maxChars {
		body = body[:maxChars]
		truncated = true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %d\n", res.Proto, res.Status)
	for k, vs := range res.Header {
		for _, v := range vs {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	b.WriteString("\n")
	b.WriteString(body)
	if truncated {
		fmt.Fprintf(&b, "\n\n... [body truncated; returned %d of %d bytes read]", len(body), len(res.Body))
	}
	fmt.Fprintf(&b, "\n\n---\nmethod=%s url=%s status=%d bytes=%d elapsed=%s redirects_followed=%t\n",
		method, rawURL, res.Status, len(res.Body), res.Elapsed.Round(time.Millisecond), a.FollowRedirects)

	return actool.Text(actool.Capture(tc, b.String())), nil
}

// newSessionTool lets the agent pin authentication for a host once, instead of
// threading a token through every call. Cookie state is handled by the shared
// jar automatically; this covers the header-based identities (Bearer, X-API-Key,
// basic auth) that a jar cannot capture.
func newSessionTool() actool.CoreTool {
	schema := obj(map[string]any{
		"action": map[string]any{
			"type":        "string",
			"enum":        []string{"set", "get", "list", "clear", "auth_check"},
			"description": "set: pin headers for a host. get: show them. list: show every host. clear: drop one host, or all when host is omitted. auth_check: send one request to the host with pinned headers and report status + body snippet — for checking whether the session is still valid (expiry detection).",
		},
		"host":    str("Host to key the session on, e.g. 127.0.0.1:3002 or app.example.com (scheme optional). For auth_check: the target URL (http://host/path is fine)."),
		"headers": map[string]any{"type": "object", "description": "Headers to pin, e.g. {\"Authorization\":\"Bearer ...\"}.", "additionalProperties": map[string]any{"type": "string"}},
	}, "action")

	desc := "Manage the per-host authentication session: pin headers (Authorization, X-API-Key, Cookie if you must) so HTTPRequest, the oracles and the crawler all send them automatically. Cookies from login responses are already persisted per host without this tool."
	return writeTool("Session", desc, schema, func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
		var a struct {
			Action  string            `json:"action"`
			Host    string            `json:"host"`
			Headers map[string]string `json:"headers"`
		}
		if err := json.Unmarshal(in, &a); err != nil {
			return actool.Errorf("Session: " + err.Error()), nil
		}
		switch strings.ToLower(strings.TrimSpace(a.Action)) {
		case "set":
			if strings.TrimSpace(a.Host) == "" {
				return actool.Errorf("Session set: host is required"), nil
			}
			if len(a.Headers) == 0 {
				return actool.Errorf("Session set: headers is required"), nil
			}
			setSessionHeaders(sessionKey(a.Host), a.Headers)
			return actool.Text(fmt.Sprintf("session headers pinned for %s: %s", sessionKey(a.Host), headerNames(a.Headers))), nil
		case "get":
			if strings.TrimSpace(a.Host) == "" {
				return actool.Errorf("Session get: host is required"), nil
			}
			h := getSessionHeaders(sessionKey(a.Host))
			raw, _ := json.Marshal(map[string]any{"host": sessionKey(a.Host), "headers": h})
			return actool.Text(string(raw)), nil
		case "list":
			all := allSessionHosts()
			raw, _ := json.Marshal(all)
			return actool.Text(string(raw)), nil
		case "clear":
			if strings.TrimSpace(a.Host) == "" {
				sessMu.Lock()
				sessHeaders = map[string]map[string]string{}
				sessMu.Unlock()
				return actool.Text("all session headers cleared"), nil
			}
			setSessionHeaders(sessionKey(a.Host), nil)
			return actool.Text("session headers cleared for " + sessionKey(a.Host)), nil
		case "auth_check":
			// Phase 4 — expiry detection. One request to the host with the pinned
			// headers; the status + body is the verdict. Auto re-login is out of
			// scope here: the result is what the agent needs.
			target := strings.TrimSpace(a.Host)
			if target == "" {
				return actool.Errorf(fmt.Sprintf("Session auth_check: host is required")), nil
			}
			host := sessionKey(target)
			res, err := doHTTP(ctx, httpCall{Method: "GET", URL: target, Headers: getSessionHeaders(host)})
			if err != nil {
				return actool.Errorf("Session auth_check: " + err.Error()), nil
			}
			snippet := string(res.Body)
			if len(snippet) > 400 {
				snippet = snippet[:400]
			}
			return actool.Text(fmt.Sprintf("auth_check host=%s status=%d len=%d body_snippet=%s\nnote: if status is 401/403 the session is expired — re-authenticate with Login, or with HTTPRequest using the Authorization header", host, res.Status, len(res.Body), strings.ReplaceAll(snippet, "\n", " "))), nil
		default:
			return actool.Errorf(fmt.Sprintf("Session: unknown action %q (use set|get|list|clear|auth_check)", a.Action)), nil
		}
	})
}

func headerNames(h map[string]string) string {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}
