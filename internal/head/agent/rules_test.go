package agent

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/stretchr/testify/assert"
)

func TestRuleEngineMatch_APIGet(t *testing.T) {
	engine := NewRuleEngine("https://api.example.com", nil)
	tc := TestCase{
		ID:     "t1",
		Target: "/api/v1/users",
		Method: "GET",
	}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	assert.Equal(t, ActionAPIRequest, action.Type)
	assert.Equal(t, "https://api.example.com/api/v1/users", action.Target)
	assert.Equal(t, "GET", action.Method)
}

func TestRuleEngineMatch_APIPost(t *testing.T) {
	engine := NewRuleEngine("https://api.example.com", nil)
	tc := TestCase{
		ID:     "t2",
		Target: "/api/v1/users",
		Method: "POST",
	}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	assert.Equal(t, "POST", action.Method)
}

func TestRuleEngineMatch_APIWithActors(t *testing.T) {
	actors := []project.Actor{
		{Name: "admin", Credentials: project.CredentialRef{Email: "admin@test.com", Password: "secret"}},
	}
	engine := NewRuleEngine("https://api.example.com", actors)
	tc := TestCase{ID: "t3", Target: "/admin/users", Method: "GET"}

	action, ok := engine.Match(tc)
	assert.True(t, ok)
	assert.Equal(t, "admin@test.com", action.Headers["X-Test-User"])
}

func TestRuleEngineMatch_Navigate(t *testing.T) {
	engine := NewRuleEngine("https://example.com", nil)
	tc := TestCase{
		ID:     "t4",
		Target: "/dashboard",
		Action: "navigate",
	}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	assert.Equal(t, ActionNavigate, action.Type)
	assert.Equal(t, "https://example.com/dashboard", action.Target)
}

func TestRuleEngineMatch_FullURL(t *testing.T) {
	engine := NewRuleEngine("https://example.com", nil)
	tc := TestCase{
		ID:     "t5",
		Target: "https://other.example.com/api/health",
	}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	assert.Equal(t, ActionNavigate, action.Type)
	assert.Equal(t, "https://other.example.com/api/health", action.Target)
}

func TestRuleEngineMatch_FullURLWithMethod(t *testing.T) {
	engine := NewRuleEngine("https://example.com", nil)
	tc := TestCase{
		ID:     "t6",
		Target: "https://api.example.com/v1/data",
		Method: "POST",
	}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	assert.Equal(t, ActionAPIRequest, action.Type)
	assert.Equal(t, "POST", action.Method)
}

func TestRuleEngineMatch_NoMatch(t *testing.T) {
	engine := NewRuleEngine("https://example.com", nil)
	tc := TestCase{
		ID:     "t7",
		Target: "verify login flow works correctly",
	}
	_, ok := engine.Match(tc)
	assert.False(t, ok)
}

func TestRuleEngineMatch_NoMatchNoMethod(t *testing.T) {
	engine := NewRuleEngine("https://example.com", nil)
	// Target is a path but no method and no navigate action.
	tc := TestCase{
		ID:     "t8",
		Target: "/some/path",
	}
	_, ok := engine.Match(tc)
	assert.False(t, ok)
}

func TestRuleEngineMatch_TrailingSlash(t *testing.T) {
	engine := NewRuleEngine("https://api.example.com/", nil)
	tc := TestCase{ID: "t9", Target: "/v1/users", Method: "GET"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	assert.Equal(t, "https://api.example.com/v1/users", action.Target)
}
