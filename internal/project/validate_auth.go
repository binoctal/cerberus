package project

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// pathParamNameRE constrains path_params keys to identifier-like names so
// they can only ever name a single {placeholder} in a URL path. Dot-path
// VALUES are unconstrained (resolved at runtime against the login response).
var pathParamNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateAuthFlow checks an AuthFlow's structural completeness: required
// fields (login.method, login.path, token_from, inject_as) are non-empty and
// inject_as contains a colon so it can split into a header name/value pair.
// It does NOT check interpolation variables against credentials — that needs
// an Actor and stays in validateAuthFlow below. Returns nil if valid.
func ValidateAuthFlow(af *AuthFlow) error {
	if af == nil {
		return errors.New("auth flow is required")
	}
	if af.Login.Method == "" {
		return errors.New("login.method is required")
	}
	if af.Login.Path == "" {
		return errors.New("login.path is required")
	}
	if af.TokenFrom == "" {
		return errors.New("token_from is required")
	}
	if af.InjectAs == "" {
		return errors.New("inject_as is required")
	}
	if !strings.Contains(af.InjectAs, ":") {
		return fmt.Errorf("inject_as %q must be a 'Name: Value' header", af.InjectAs)
	}
	// path_params keys must be identifier-like (a single {placeholder} name);
	// dot-path values are unconstrained (resolved at runtime).
	for name := range af.PathParams {
		if !pathParamNameRE.MatchString(name) {
			return fmt.Errorf("path_params: key %q is not a valid param name (must match %s)", name, pathParamNameRE.String())
		}
	}
	return nil
}

// validateAuthFlow validates an actor's optional declarative auth block for
// config-time errors. Returns the first problem as a string (validation
// collects per-actor into ValidationError).
func validateAuthFlow(actorIdx int, a Actor) string {
	if a.Auth == nil {
		return ""
	}
	prefix := fmt.Sprintf("actors[%d].auth", actorIdx)
	// Provisioning-only flow (Task 4): when the actor has a static
	// Credentials.Token, token_from may be empty — login runs only to capture
	// path params, and the static token is injected as RawToken. ValidateAuthFlow
	// is strict (it serves authdiscover, which has no static token), so bypass
	// its token_from requirement here by substituting a sentinel for validation.
	af := a.Auth
	if af.TokenFrom == "" && a.Credentials.Token != "" {
		clone := *af
		clone.TokenFrom = "<static>"
		af = &clone
	}
	if err := ValidateAuthFlow(af); err != nil {
		return prefix + "." + err.Error()
	}
	// Every {email}/{password} referenced in login.body must have a matching
	// non-empty credential field; otherwise login would interpolate to "" at
	// runtime with no warning.
	for _, v := range a.Auth.Login.Body {
		for _, ref := range []string{"{email}", "{password}"} {
			if strings.Contains(v, ref) {
				field := ref[1 : len(ref)-1] // "email" or "password"
				if credentialField(a.Credentials, field) == "" {
					return fmt.Sprintf("%s: body references interpolation variable %s but credentials.%s is empty", prefix, ref, field)
				}
			}
		}
	}
	return ""
}

// credentialField returns the named credential value by field name.
func credentialField(c CredentialRef, field string) string {
	switch field {
	case "email":
		return c.Email
	case "password":
		return c.Password
	default:
		return ""
	}
}
