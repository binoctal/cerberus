package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// authHeaders includes the actor's Credentials.Headers (e.g. Authorization),
// alongside the legacy X-Test-User.
func TestRuleEngine_AuthHeadersIncludesActorHeaders(t *testing.T) {
	services := []project.Service{{Name: "default", URL: "http://localhost"}}
	engine := NewRuleEngine(services, []project.Actor{{
		Name: "valid_user",
		Credentials: project.CredentialRef{
			Email:   "x@y.z",
			Headers: map[string]string{"Authorization": "Bearer sk-test"},
		},
	}}, "")

	h := engine.authHeadersFor(TestCase{})
	assert.Equal(t, "x@y.z", h["X-Test-User"])
	assert.Equal(t, "Bearer sk-test", h["Authorization"])
}

// An actor with headers but no email still yields its headers (needed for
// bearer-only actors that aren't SaaS email/password).
func TestRuleEngine_AuthHeadersHeadersOnlyNoEmail(t *testing.T) {
	services := []project.Service{{Name: "default", URL: "http://localhost"}}
	engine := NewRuleEngine(services, []project.Actor{{
		Name: "valid_user",
		Credentials: project.CredentialRef{
			Headers: map[string]string{"Authorization": "Bearer sk-test"},
		},
	}}, "")

	h := engine.authHeadersFor(TestCase{})
	assert.Equal(t, "Bearer sk-test", h["Authorization"])
	assert.Equal(t, "", h["X-Test-User"])
}

// ReAct path: the active actor's headers are merged under the action's own
// headers so a header-less steer() output still authenticates.
func TestReActLoop_WithActorHeadersMerged(t *testing.T) {
	services := []project.Service{{Name: "default", URL: "http://localhost"}}
	engine := NewRuleEngine(services, []project.Actor{{
		Credentials: project.CredentialRef{
			Headers: map[string]string{"Authorization": "Bearer sk-actor"},
		},
	}}, "")
	loop := &ReActLoop{engine: engine}

	tc := TestCase{ID: "t1"}

	// No action header → gets the actor's.
	out := loop.withActorHeaders(tc, types.HTTPAction{Method: "GET", URL: "http://x/y"})
	assert.Equal(t, "Bearer sk-actor", out.(types.HTTPAction).Headers["Authorization"])

	// Action's own value overrides.
	out2 := loop.withActorHeaders(tc, types.HTTPAction{
		Method:  "GET",
		URL:     "http://x/y",
		Headers: map[string]string{"Authorization": "Bearer override"},
	})
	assert.Equal(t, "Bearer override", out2.(types.HTTPAction).Headers["Authorization"])

	// Empty action value removes the actor header (negative "no auth" case).
	out3 := loop.withActorHeaders(tc, types.HTTPAction{
		Method:  "GET",
		URL:     "http://x/y",
		Headers: map[string]string{"Authorization": ""},
	})
	_, present := out3.(types.HTTPAction).Headers["Authorization"]
	assert.False(t, present)
}

// ReAct path selects the actor whose Service matches tc.Service, mirroring the
// rule engine — not actors[0].
func TestReActLoop_WithActorHeadersPerService(t *testing.T) {
	services := []project.Service{
		{Name: "a", URL: "http://a"},
		{Name: "b", URL: "http://b"},
	}
	actors := []project.Actor{
		{Service: "a", Credentials: project.CredentialRef{Headers: map[string]string{"Authorization": "Bearer sk-a"}}},
		{Service: "b", Credentials: project.CredentialRef{Headers: map[string]string{"Authorization": "Bearer sk-b"}}},
	}
	engine := NewRuleEngine(services, actors, "")
	loop := &ReActLoop{engine: engine}

	out := loop.withActorHeaders(TestCase{ID: "t1", Service: "b"}, types.HTTPAction{Method: "GET", URL: "http://b/y"})
	assert.Equal(t, "Bearer sk-b", out.(types.HTTPAction).Headers["Authorization"])

	// Service "a" does NOT leak actor b's credentials.
	outA := loop.withActorHeaders(TestCase{ID: "t2", Service: "a"}, types.HTTPAction{Method: "GET", URL: "http://a/y"})
	assert.Equal(t, "Bearer sk-a", outA.(types.HTTPAction).Headers["Authorization"])
}

// ReAct path injects X-Test-User from the selected actor's email, matching the
// rule path (previously dropped on the ReAct path).
func TestReActLoop_WithActorHeadersInjectsXTestUser(t *testing.T) {
	services := []project.Service{{Name: "a", URL: "http://a"}}
	engine := NewRuleEngine(services, []project.Actor{{
		Service:     "a",
		Credentials: project.CredentialRef{Email: "u@a"},
	}}, "")
	loop := &ReActLoop{engine: engine}

	out := loop.withActorHeaders(TestCase{Service: "a"}, types.HTTPAction{Method: "GET", URL: "http://a/y"})
	assert.Equal(t, "u@a", out.(types.HTTPAction).Headers["X-Test-User"])
}

// ReAct path falls back to a global actor (Service == "") when no actor matches
// tc.Service, then to actors[0] when no global actor exists.
func TestReActLoop_WithActorHeadersFallbacks(t *testing.T) {
	services := []project.Service{{Name: "a", URL: "http://a"}}
	specific := project.Actor{Service: "x", Credentials: project.CredentialRef{Headers: map[string]string{"Authorization": "Bearer sk-x"}}}
	global := project.Actor{Credentials: project.CredentialRef{Headers: map[string]string{"Authorization": "Bearer sk-global"}}}

	// Global-actor fallback: tc.Service matches no actor, but a global actor
	// exists (and is not actors[0]) — selection must skip the specific actor.
	engine := NewRuleEngine(services, []project.Actor{specific, global}, "")
	loop := &ReActLoop{engine: engine}
	out := loop.withActorHeaders(TestCase{Service: "missing"}, types.HTTPAction{Method: "GET", URL: "http://a/y"})
	assert.Equal(t, "Bearer sk-global", out.(types.HTTPAction).Headers["Authorization"])

	// actors[0] fallback: no matching actor and no global actor.
	engine2 := NewRuleEngine(services, []project.Actor{specific}, "")
	loop2 := &ReActLoop{engine: engine2}
	out2 := loop2.withActorHeaders(TestCase{Service: "missing"}, types.HTTPAction{Method: "GET", URL: "http://a/y"})
	assert.Equal(t, "Bearer sk-x", out2.(types.HTTPAction).Headers["Authorization"])
}

// An actor that captured an http_login JWT (RawHTTPToken) must use it for HTTP
// route auth, overriding the WS web-token carried in Headers. Without this, a
// rule-engine HTTP case (method + path) for a declared-http_login actor (e.g.
// the dogfood web-actor) injects the WS demo_token and 401s on protected HTTP
// routes — the "Scout drifts to web-token" failure.
func TestRuleEngine_AuthHeadersPrefersHTTPTokenOverWebToken(t *testing.T) {
	services := []project.Service{{Name: "realtime", URL: "http://localhost"}}
	engine := NewRuleEngine(services, []project.Actor{{
		Service: "realtime",
		Credentials: project.CredentialRef{
			Headers:      map[string]string{"Authorization": "Bearer demo_token"},
			RawHTTPToken: "jwt-from-http-login",
		},
	}}, "")

	h := engine.authHeadersFor(TestCase{Service: "realtime"})
	assert.Equal(t, "Bearer jwt-from-http-login", h["Authorization"],
		"rule-engine HTTP path must inject the http_login JWT, not the WS web-token")
}

// Non-HTTP actions pass through untouched.
func TestReActLoop_WithActorHeadersNonHTTP(t *testing.T) {
	services := []project.Service{{Name: "default", URL: "http://localhost"}}
	engine := NewRuleEngine(services, []project.Actor{{
		Credentials: project.CredentialRef{
			Headers: map[string]string{"Authorization": "Bearer sk-actor"},
		},
	}}, "")
	loop := &ReActLoop{engine: engine}

	tc := TestCase{ID: "t1"}
	in := types.NavigateAction{URL: "http://x/y"}
	out := loop.withActorHeaders(tc, in)
	assert.Equal(t, in, out)
}
