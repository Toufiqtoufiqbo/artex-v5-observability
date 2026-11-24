package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	actool "github.com/Autumn-27/norma/tool"
)

// Login attempts an authentication POST (or GET) and stores the identity with
// the host. Two identity paths work, both useful in practice:
//   - the browser-style path: the response sets a cookie → shared jar replayed
//     by every later HTTPRequest to the same host (see TestSessionCookieJar)
//   - the API-style path: the response returns a token → it is pinned via
//     setSessionHeaders so every later call sends Authorization
// The verdict "authenticated" is decided from the response body, and the raw
// exchange is always returned so it can be cited as evidence.
func newLoginTool() actool.CoreTool {
	schema := obj(map[string]any{
		"url": str("Absolute login endpoint URL."),
		"username": str("Username or email."),
		"password": str("Password."),
		"username_param": map[string]any{
			"type":        "string",
			"description": "Body/query parameter name for the username. Default: \"username\". For email login pass the actual param name.",
			"default":     "username",
		},
		"password_param":   map[string]any{"type": "string", "description": "Default: \"password\".", "default": "password"},
		"method":           map[string]any{"type": "string", "enum": []string{"POST", "GET"}, "description": "Default POST."},
		"json":             map[string]any{"type": "boolean", "description": "Send the credentials as a JSON body (default form-encoded). Use when the endpoint expects JSON."},
		"headers":          map[string]any{"type": "object", "description": "Extra request headers.", "additionalProperties": map[string]any{"type": "string"}},
		"timeout_ms":       intp("Timeout in ms (default 30000)."),
		"insecure":         map[string]any{"type": "boolean", "description": "Skip TLS verification."},
	}, "url")

	desc := "Attempt an authentication login against a target you have credentials for. The cookie (browser path) or returned token (API path) is stored and replayed automatically by HTTPRequest and the oracles. Returns the raw exchange plus an \"authenticated\" verdict derived from the response, to be cited as evidence."
	return writeTool("Login", desc, schema, func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
		var a struct {
			URL           string            `json:"url"`
			Username      string            `json:"username"`
			Password      string            `json:"password"`
			UsernameParam string            `json:"username_param"`
			PasswordParam string            `json:"password_param"`
			Method        string            `json:"method"`
			JSON          bool              `json:"json"`
			Headers       map[string]string `json:"headers"`
			TimeoutMS     int               `json:"timeout_ms"`
			Insecure      bool              `json:"insecure"`
		}
		if err := json.Unmarshal(in, &a); err != nil {
			return actool.Errorf("Login: " + err.Error()), nil
		}
		if strings.TrimSpace(a.URL) == "" {
			return actool.Errorf("Login: url is required"), nil
		}
		if strings.TrimSpace(a.Username) == "" || strings.TrimSpace(a.Password) == "" {
			return actool.Errorf("Login: username and password are required"), nil
		}
		up := strings.TrimSpace(a.UsernameParam)
		if up == "" {
			up = "username"
		}
		pp := strings.TrimSpace(a.PasswordParam)
		if pp == "" {
			pp = "password"
		}
		method := strings.ToUpper(strings.TrimSpace(a.Method))
		if method == "" {
			method = "POST"
		}

		var body string
		var callURL = a.URL
		switch {
		case method == "GET":
			q1, err := injectQuery(a.URL, up, a.Username)
			if err != nil {
				return actool.Errorf("Login: " + err.Error()), nil
			}
			callURL, err = injectQuery(q1, pp, a.Password)
			if err != nil {
				return actool.Errorf("Login: " + err.Error()), nil
			}
		case a.JSON:
			obj := map[string]string{up: a.Username, pp: a.Password}
			b, _ := json.Marshal(obj)
			body = string(b)
		default:
			body = loginFormEncode(up, a.Username, pp, a.Password)
		}

		call := httpCall{
			Method: method, URL: callURL, Headers: a.Headers, Body: body,
			Insecure: a.Insecure, Timeout: time.Duration(a.TimeoutMS) * time.Millisecond,
		}
		if call.Timeout <= 0 {
			call.Timeout = httpDefaultTimeout
		}

		res, err := doHTTP(ctx, call)
		if err != nil {
			return actool.Errorf("Login: " + err.Error()), nil
		}

		resp := string(res.Body)
		snippet := resp
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}

		// Browser path: the jar already stores the cookies from this response.
		// API path: pin the returned token as a session header.
		host := sessionKey(a.URL)
		var token string
		var aux map[string]any
		if json.Unmarshal([]byte(resp), &aux) == nil {
			for _, key := range []string{"token", "access_token", "jwt"} {
				if s, ok := aux[key].(string); ok && s != "" {
					token = s
					break
				}
			}
		}
		if token != "" {
			setSessionHeaders(host, map[string]string{"Authorization": "Bearer " + token})
		}

		// Verdict: 2xx and the body doesn't say the credentials were rejected.
		// Some login APIs return 200 with an error payload, some return 401 —
		// combining both signals keeps the verdict useful.
		authenticated := res.Status >= 200 && res.Status < 400 && !looksFailed(resp)
		if token != "" {
			authenticated = true
		}

		var b strings.Builder
		fmt.Fprintf(&b, "login status=%d url=%s username_param=%s password_param=%s method=%s\n", res.Status, a.URL, up, pp, method)
		fmt.Fprintf(&b, "authenticated=%t\n", authenticated)
		fmt.Fprintf(&b, "response_snippet: %s\n", strings.ReplaceAll(snippet, "\n", " "))
		if token != "" {
			fmt.Fprintf(&b, "note: token stored as Authorization header for %s (will be replayed by HTTPRequest/Probes/Crawl)\n", host)
		} else {
			fmt.Fprintf(&b, "note: cookies from this response are stored in the shared session jar for %s\n", host)
		}
		return actool.Text(b.String()), nil
	})
}

func looksFailed(resp string) bool {
	low := strings.ToLower(strings.TrimSpace(resp))
	for _, bad := range []string{
		"invalid", "incorrect", "denied", "unauthorized", "authentication failed",
		"invalid credentials", "not authenticated", "failed to authenticate",
		"bad username", "bad password", "login failed", "error",
	} {
		if strings.Contains(low, bad) {
			return true
		}
	}
	return false
}

func loginFormEncode(up, username, pp, password string) string {
	v := strings.Builder{}
	v.WriteString(loginEscape(up) + "=" + loginEscape(username))
	if v.Len() > 0 {
	}
	v.WriteString("&" + loginEscape(pp) + "=" + loginEscape(password))
	return v.String()
}

// loginEscape is a minimal form-url encoder for the two credential values.
func loginEscape(s string) string {
	r := strings.NewReplacer("%", "%25", "+", "%2B", "&", "%26", "=", "%3D", " ", "+", "#", "%23")
	return r.Replace(s)
}
