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

	// No action header → gets the actor's.
	out := loop.withActorHeaders(types.HTTPAction{Method: "GET", URL: "http://x/y"})
	assert.Equal(t, "Bearer sk-actor", out.(types.HTTPAction).Headers["Authorization"])

	// Action's own value overrides.
	out2 := loop.withActorHeaders(types.HTTPAction{
		Method:  "GET",
		URL:     "http://x/y",
		Headers: map[string]string{"Authorization": "Bearer override"},
	})
	assert.Equal(t, "Bearer override", out2.(types.HTTPAction).Headers["Authorization"])

	// Empty action value removes the actor header (negative "no auth" case).
	out3 := loop.withActorHeaders(types.HTTPAction{
		Method:  "GET",
		URL:     "http://x/y",
		Headers: map[string]string{"Authorization": ""},
	})
	_, present := out3.(types.HTTPAction).Headers["Authorization"]
	assert.False(t, present)
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

	in := types.NavigateAction{URL: "http://x/y"}
	out := loop.withActorHeaders(in)
	assert.Equal(t, in, out)
}
