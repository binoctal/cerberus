package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// ResolveAuthHeader runs an actor's declarative login flow once and returns the
// header name and value to inject into subsequent requests. It is called at
// session setup, once per actor that has an Auth block; the result is cached
// for the session by writing it into the actor's Credentials.Headers.
//
// On any failure (network error, non-2xx response, missing token field) it
// returns an error; the caller logs a warning and leaves the actor
// unauthenticated rather than aborting the session.
//
// Token values are never returned in errors or logged by this function — the
// caller records only the header name, HTTP status, and length.
func ResolveAuthHeader(ctx context.Context, svcURL string, actor project.Actor) (name, value string, err error) {
	af := actor.Auth
	if af == nil {
		return "", "", fmt.Errorf("actor has no auth flow")
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

	// 2. Build the login URL: absolute path wins, else join onto svcURL.
	loginURL := af.Login.Path
	if !isAbsoluteURL(loginURL) {
		loginURL = strings.TrimRight(svcURL, "/") + "/" + strings.TrimLeft(loginURL, "/")
	}

	var bodyReader io.Reader
	if len(bodyFields) > 0 {
		encoded, mErr := json.Marshal(bodyFields)
		if mErr != nil {
			return "", "", fmt.Errorf("auth flow: encode login body: %w", mErr)
		}
		bodyReader = strings.NewReader(string(encoded))
	}

	method := af.Login.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, loginURL, bodyReader)
	if err != nil {
		return "", "", fmt.Errorf("auth flow: build request: %w", err)
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
		return "", "", fmt.Errorf("auth flow: login request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Non-2xx: never include the body (may echo credentials).
		return "", "", fmt.Errorf("auth flow: login returned status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("auth flow: read response: %w", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", "", fmt.Errorf("auth flow: response is not a JSON object")
	}

	// 4. Extract the token by dot-path.
	token, err := extractByDotPath(decoded, af.TokenFrom)
	if err != nil {
		return "", "", err
	}

	// 5. Interpolate {token} into inject_as and split into header name/value.
	header := interpolate(af.InjectAs, map[string]string{"{token}": token})
	hName, hValue, ok := splitHeader(header)
	if !ok {
		return "", "", fmt.Errorf("auth flow: inject_as %q is not a 'Name: Value' header", af.InjectAs)
	}
	return hName, hValue, nil
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
