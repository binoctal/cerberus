package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/types"
)

func TestFallbackParseAction_ClickKeyword(t *testing.T) {
	action := FallbackParseAction("I should click on the submit button", "#submit")
	_, ok := action.(types.NavigateAction)
	assert.True(t, ok, "expected NavigateAction for click keyword")
}

func TestFallbackParseAction_PostKeyword(t *testing.T) {
	action := FallbackParseAction("POST to /api/v1/users with the form data", "/api/v1/users")
	httpAct, ok := action.(types.HTTPAction)
	assert.True(t, ok, "expected HTTPAction for post keyword")
	assert.Equal(t, "POST", httpAct.Method)
}

func TestFallbackParseAction_GetKeyword(t *testing.T) {
	action := FallbackParseAction("Let me GET /health endpoint", "/health")
	httpAct, ok := action.(types.HTTPAction)
	assert.True(t, ok, "expected HTTPAction for get keyword")
	assert.Equal(t, "GET", httpAct.Method)
}

func TestFallbackParseAction_NavigateKeyword(t *testing.T) {
	action := FallbackParseAction("Navigate to the dashboard page", "/dashboard")
	_, ok := action.(types.NavigateAction)
	assert.True(t, ok, "expected NavigateAction for navigate keyword")
}

func TestFallbackParseAction_NoKeyword(t *testing.T) {
	action := FallbackParseAction("The page shows an error message", "/some/page")
	waitAct, ok := action.(types.WaitAction)
	assert.True(t, ok, "expected WaitAction for no keyword")
	assert.Equal(t, "1s", waitAct.Duration)
}

func TestFallbackParseAction_EmptyInput(t *testing.T) {
	action := FallbackParseAction("", "/default")
	waitAct, ok := action.(types.WaitAction)
	assert.True(t, ok, "expected WaitAction for empty input")
	assert.Equal(t, "1s", waitAct.Duration)
}

func TestFallbackParseAction_WhitespaceOnly(t *testing.T) {
	action := FallbackParseAction("  \n  \n  ", "/target")
	_, ok := action.(types.WaitAction)
	assert.True(t, ok, "expected WaitAction for whitespace only")
}

func TestFallbackParseAction_CaseInsensitive(t *testing.T) {
	action := FallbackParseAction("DELETE the resource at /api/items/1", "/api/items/1")
	httpAct, ok := action.(types.HTTPAction)
	assert.True(t, ok, "expected HTTPAction for delete keyword")
	assert.Equal(t, "DELETE", httpAct.Method)
}
