package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/binoctal/cerberus/internal/project"
)

// httpClient is used by ResolveAuthHeader so tests can swap in an
// httptest.Server-aware client. It carries a conservative timeout so a hung
// login endpoint cannot stall session setup indefinitely.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// interpolate replaces every "{name}" token in template with the matching
// value from vars. Unknown tokens are left untouched.
func interpolate(template string, vars map[string]string) string {
	out := template
	for k, v := range vars {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

// extractByDotPath walks a decoded JSON object by a dotted path (e.g.
// "data.accessToken") and returns the string form of the leaf value. Returns
// an error naming the missing or non-traversable key so callers can log it.
func extractByDotPath(data map[string]any, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty token_from path")
	}
	cur := any(data)
	for _, key := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("token_from: key %q is not an object", key)
		}
		next, exists := obj[key]
		if !exists {
			return "", fmt.Errorf("token_from: key %q not found", key)
		}
		cur = next
	}
	switch v := cur.(type) {
	case string:
		return v, nil
	case fmt.Stringer:
		return v.String(), nil
	default:
		return fmt.Sprint(v), nil
	}
}

// AuthResult is the outcome of an actor's auth flow: the header to inject, the
// raw token (for WS query/header/subprotocol), and any captured URL path
// params. Token values are never placed in errors or logs by this package.
type AuthResult struct {
	HeaderName  string
	HeaderValue string
	RawToken    string
	PathParams  map[string]string // url-param -> captured value (F3); nil when no path_params declared
}

// ResolveAuthHeader runs an actor's declarative login flow once and returns the
// header name and value to inject into subsequent requests, plus any captured
// URL path params. It is called at session setup, once per actor that has an
// Auth block; the result is cached for the session by writing it into the
// actor's Credentials.
//
// On any failure (network error, non-2xx response, missing token field) it
// returns an error; the caller logs a warning and leaves the actor
// unauthenticated rather than aborting the session.
//
// Token values are never returned in errors or logged by this function — the
// caller records only the header name, HTTP status, and length. RawToken is
// the unformatted credential extracted from the login response; it is a value,
// never logged, and is cached by the caller for WS auth injection. PathParams
// are captured from the same login response; an absent dot-path yields ""
// (non-fatal) — connect-time {param} resolution (Task 2) fails clearly on an
// empty/missing value rather than failing the whole auth flow here.
func ResolveAuthHeader(ctx context.Context, svcURL string, actor project.Actor) (*AuthResult, error) {
	af := actor.Auth
	if af == nil {
		return nil, fmt.Errorf("actor has no auth flow")
	}

	// 1. Interpolate {email}/{password} into the login body.
	vars := map[string]string{
		"{email}":    actor.Credentials.Email,
		"{password}": actor.Credentials.Password,
	}
	bodyFields := make(map[string]string, len(af.Login.Body))
	for k, v := range af.Login.Body {
		bodyFields[k] = interpolate(v, vars)
	}

	// 2. Build the login URL: absolute path wins, else join onto the
	// service URL's scheme+host only. The service URL's path component
	// (e.g. a WS route template "/ws/{userId}") is intentionally dropped:
	// login is host-relative, and {param} placeholders cannot be resolved
	// before login runs.
	loginURL := af.Login.Path
	if !isAbsoluteURL(loginURL) {
		var base string
		if u, err := url.Parse(svcURL); err == nil && u.IsAbs() {
			base = u.Scheme + "://" + u.Host
		} else {
			base = strings.TrimRight(svcURL, "/")
		}
		loginURL = base + "/" + strings.TrimLeft(loginURL, "/")
	}

	var bodyReader io.Reader
	if len(bodyFields) > 0 {
		encoded, mErr := json.Marshal(bodyFields)
		if mErr != nil {
			return nil, fmt.Errorf("auth flow: encode login body: %w", mErr)
		}
		bodyReader = strings.NewReader(string(encoded))
	}

	method := af.Login.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, loginURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("auth flow: build request: %w", err)
	}
	if len(bodyFields) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range af.Login.Headers {
		req.Header.Set(k, v)
	}

	// 3. Send one real request.
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth flow: login request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Non-2xx: never include the body (may echo credentials).
		return nil, fmt.Errorf("auth flow: login returned status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("auth flow: read response: %w", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("auth flow: response is not a JSON object")
	}

	// 4. Token resolution: an explicit token_from dot-path reads the login
	// response; an EMPTY token_from makes this a provisioning-only flow — the
	// static Credentials.Token is used, but login still runs to capture
	// PathParams (e.g. a provisioned userId for URL templating).
	token := actor.Credentials.Token
	if af.TokenFrom != "" {
		t, err := extractByDotPath(decoded, af.TokenFrom)
		if err != nil {
			return nil, err
		}
		token = t
	}

	// 5. F3: capture declared path params from the same response. An absent
	// dot-path yields "" (non-fatal); connect-time {param} resolution fails
	// clearly on an empty value rather than failing the whole auth here.
	var pathParams map[string]string
	for name, dotPath := range af.PathParams {
		if pathParams == nil {
			pathParams = make(map[string]string)
		}
		if v, pErr := extractByDotPath(decoded, dotPath); pErr == nil {
			pathParams[name] = v
		} else {
			pathParams[name] = ""
		}
	}

	// 6. Interpolate {token} into inject_as and split into header name/value.
	header := interpolate(af.InjectAs, map[string]string{"{token}": token})
	hName, hValue, ok := splitHeader(header)
	if !ok {
		return nil, fmt.Errorf("auth flow: inject_as %q is not a 'Name: Value' header", af.InjectAs)
	}
	return &AuthResult{HeaderName: hName, HeaderValue: hValue, RawToken: token, PathParams: pathParams}, nil
}

// splitHeader splits "Name: Value" into name and value at the first colon.
// Value is space-trimmed. Returns ok=false if there is no colon.
func splitHeader(s string) (name, value string, ok bool) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:]), true
}
