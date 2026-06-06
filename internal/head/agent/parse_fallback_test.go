package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFallbackParseAction_ClickKeyword(t *testing.T) {
	action := FallbackParseAction("I should click on the submit button", "#submit")
	assert.Equal(t, ActionClick, action.Type)
	assert.Equal(t, "#submit", action.Target)
}

func TestFallbackParseAction_PostKeyword(t *testing.T) {
	action := FallbackParseAction("POST to /api/v1/users with the form data", "/api/v1/users")
	assert.Equal(t, ActionAPIRequest, action.Type)
	assert.Equal(t, "/api/v1/users", action.Target)
}

func TestFallbackParseAction_GetKeyword(t *testing.T) {
	action := FallbackParseAction("Let me GET /health endpoint", "/health")
	assert.Equal(t, ActionAPIRequest, action.Type)
}

func TestFallbackParseAction_NavigateKeyword(t *testing.T) {
	action := FallbackParseAction("Navigate to the dashboard page", "/dashboard")
	assert.Equal(t, ActionNavigate, action.Type)
}

func TestFallbackParseAction_NoKeyword(t *testing.T) {
	action := FallbackParseAction("The page shows an error message", "/some/page")
	assert.Equal(t, ActionWait, action.Type)
	assert.Equal(t, "1s", action.Value)
}

func TestFallbackParseAction_EmptyInput(t *testing.T) {
	action := FallbackParseAction("", "/default")
	assert.Equal(t, ActionWait, action.Type)
	assert.Equal(t, "/default", action.Target)
	assert.Equal(t, "1s", action.Value)
}

func TestFallbackParseAction_WhitespaceOnly(t *testing.T) {
	action := FallbackParseAction("  \n  \n  ", "/target")
	assert.Equal(t, ActionWait, action.Type)
}

func TestFallbackParseAction_CaseInsensitive(t *testing.T) {
	action := FallbackParseAction("DELETE the resource at /api/items/1", "/api/items/1")
	assert.Equal(t, ActionAPIRequest, action.Type)
}
