package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseStructuredOutput_FencedJSON reproduces a real failure seen when a
// non-Claude LLM wraps a valid JSON object in a ```json markdown fence. All
// three parse phases previously returned false, so the caller saw
// "failed to parse structured output: invalid character '`'". The fenced input
// must parse successfully.
func TestParseStructuredOutput_FencedJSON(t *testing.T) {
	// Shape mirrors the coverage contract the Scout prompt asks for.
	input := "```json\n" + `{
  "depth": "standard",
  "scope": ["GET /"],
  "path_types": ["happy", "alternative"],
  "error_scope": ["4xx", "validation"],
  "boundaries": ["empty", "zero", "max", "invalid"],
  "priorities": {"health_check": "critical", "root_response": "high"},
  "coverage_gate": {"module": "build_selftest", "line_threshold": 100.0}
}` + "\n```"

	// Isolate parser vs target-struct: decode into a generic map first.
	var generic map[string]any
	require.NoError(t, ParseStructuredOutput(input, &generic),
		"fenced JSON must parse; input was:\n%s", input)
	assert.Equal(t, "standard", generic["depth"])

	// Decode into a struct shaped like contract.Contract to confirm field mapping.
	var typed struct {
		Depth        string   `json:"depth"`
		Scope        []string `json:"scope"`
		CoverageGate struct {
			Module        string  `json:"module"`
			LineThreshold float64 `json:"line_threshold"`
		} `json:"coverage_gate"`
	}
	require.NoError(t, ParseStructuredOutput(input, &typed))
	assert.Equal(t, "standard", typed.Depth)
	assert.InDelta(t, 100.0, typed.CoverageGate.LineThreshold, 0.001)
}

// TestParseStructuredOutput_FenceWithoutTrailingNewline covers the fence shape
// some models emit where the closing fence immediately follows the JSON with no
// blank line, which the original regex required.
func TestParseStructuredOutput_FenceWithoutTrailingNewline(t *testing.T) {
	input := "```json\n{\"a\": 1}```"
	var got struct {
		A int `json:"a"`
	}
	// Sanity: the inner JSON is valid once unwrapped.
	require.NoError(t, json.Unmarshal([]byte("{\"a\": 1}"), &got))
	got.A = 0

	require.NoError(t, ParseStructuredOutput(input, &got))
	assert.Equal(t, 1, got.A)
}

// TestTryPhases documents which phase handles which shape, guarding against
// regressions in the fallback ladder.
func TestTryPhases(t *testing.T) {
	t.Run("bare json", func(t *testing.T) {
		var got struct {
			A int `json:"a"`
		}
		assert.True(t, tryDirectJSON(`{"a":1}`, &got))
		assert.Equal(t, 1, got.A)
	})
	t.Run("fenced json", func(t *testing.T) {
		var got struct {
			A int `json:"a"`
		}
		assert.True(t, tryMarkdownJSON("```json\n{\"a\":1}\n```", &got), "input: %s", fenced())
		assert.Equal(t, 1, got.A)
	})
}

func fenced() string {
	return strings.Repeat("`", 3) + `json` + "\n{\"a\":1}\n" + strings.Repeat("`", 3)
}
