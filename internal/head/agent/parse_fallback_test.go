package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/types"
)

func TestFallbackParseAction_ClickKeyword(t *testing.T) {
	action := FallbackParseAction("I should click on the submit button", "#submit")
	browserAct, ok := action.(types.BrowserClickAction)
	assert.True(t, ok, "expected BrowserClickAction for click keyword")
	assert.Equal(t, "#submit", browserAct.Selector)
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

func TestFallbackParseAction_TypeKeyword(t *testing.T) {
	action := FallbackParseAction("I need to type in the username field", "#username")
	fillAct, ok := action.(types.BrowserFillAction)
	assert.True(t, ok, "expected BrowserFillAction for type keyword")
	assert.Equal(t, "#username", fillAct.Selector)
}

func TestFallbackParseAction_FillKeyword(t *testing.T) {
	action := FallbackParseAction("Fill in the password field", "#password")
	fillAct, ok := action.(types.BrowserFillAction)
	assert.True(t, ok, "expected BrowserFillAction for fill keyword")
	assert.Equal(t, "#password", fillAct.Selector)
}

func TestFallbackParseAction_GotoKeyword(t *testing.T) {
	action := FallbackParseAction("Goto the settings page", "/settings")
	navAct, ok := action.(types.NavigateAction)
	assert.True(t, ok, "expected NavigateAction for goto keyword")
	assert.Equal(t, "/settings", navAct.URL)
}

func TestFallbackParseAction_ErrorMessageCleanup(t *testing.T) {
	// Test that fallback properly handles error messages with raw content dump
	errorMsg := "parse output: unexpected end of JSON input\nraw: {\"type\":\"navigate\",\"url\":\"/test\"}"
	action := FallbackParseAction(errorMsg, "/default")
	// Should extract meaningful intent from error message, not fail on raw content
	waitAct, ok := action.(types.WaitAction)
	assert.True(t, ok, "expected WaitAction when no clear intent in error message")
	assert.Equal(t, "1s", waitAct.Duration)
}
