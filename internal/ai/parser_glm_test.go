package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseStructuredOutput_GLMJudgeFenced reproduces the real examiner failure
// from a GLM-driven L2 run: the model wraps its verdict in a ```json fence AND
// the reasoning string contains a nested JSON body with braces.
func TestParseStructuredOutput_GLMJudgeFencedWithNestedJSON(t *testing.T) {
	input := "```json\n" + `{
  "status": "pass",
  "existence_confidence": 1.0,
  "correctness_confidence": 0.95,
  "reasoning": "The HTTP Status was 404. The response body '{\"error\":{\"type\":\"invalid_request_error\",\"message\":\"unknown domain\"}}' explicitly confirms the condition."
}` + "\n```"

	var got struct {
		Status      string  `json:"status"`
		Existence   float64 `json:"existence_confidence"`
		Correctness float64 `json:"correctness_confidence"`
		Reasoning   string  `json:"reasoning"`
	}
	require.NoError(t, ParseStructuredOutput(input, &got), "input was:\n%s", input)
	assert.Equal(t, "pass", got.Status)
	assert.Contains(t, got.Reasoning, "unknown domain")
}

// TestParseStructuredOutput_GLMRobustness covers GLM output shapes that the
// original parser handled inconsistently. Each must parse into a map.
func TestParseStructuredOutput_GLMRobustness(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"plain fenced", "```json\n{\"a\":1}\n```"},
		{"nested braces in string", "```json\n{\"a\":\"x '{\\\"b\\\":1}' y\"}\n```"},
		{"leading prose before fence", "Here is the verdict:\n```json\n{\"a\":1}\n```"},
		{"trailing prose after fence", "```json\n{\"a\":1}\n```\nDone."},
		{"crlf line endings", "```json\r\n{\"a\":1}\r\n```"},
		{"no json tag on fence", "```\n{\"a\":1}\n```"},
		// GLM frequently emits literal newlines inside a long string value
		// (e.g. multi-line reasoning), which is invalid JSON. The parser must
		// recover by escaping them.
		{"literal newline inside string value", "```json\n{\"a\":\"line one\nline two\"}\n```"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var m map[string]json.RawMessage
			err := ParseStructuredOutput(c.input, &m)
			require.NoErrorf(t, err, "input: %q", c.input)
			assert.NotNil(t, m["a"], c.name)
		})
	}
}
