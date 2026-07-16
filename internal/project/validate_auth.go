package project

import (
	"fmt"
	"strings"
)

// validateAuthFlow validates an actor's optional declarative auth block.
// All checks are config-time so a misconfigured flow is never a runtime
// surprise. Returns the first problem found (validation collects per-actor).
func validateAuthFlow(actorIdx int, a Actor) string {
	if a.Auth == nil {
		return ""
	}
	af := a.Auth
	prefix := fmt.Sprintf("actors[%d].auth", actorIdx)
	if af.Login.Method == "" {
		return prefix + ".login.method is required"
	}
	if af.Login.Path == "" {
		return prefix + ".login.path is required"
	}
	if af.TokenFrom == "" {
		return prefix + ".token_from is required"
	}
	if af.InjectAs == "" {
		return prefix + ".inject_as is required"
	}
	// Every {email}/{password} referenced in login.body must have a matching
	// non-empty credential field; otherwise login would interpolate to "" at
	// runtime with no warning.
	for _, v := range af.Login.Body {
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
