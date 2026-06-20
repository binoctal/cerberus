package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/ai"
)

// TestAnalyzeOutput_FlexibleTechStack guards against the dogfood regression
// where a real LLM wraps output in ```json fences and emits tech_stack as an
// array of objects with confidence rather than the documented string array.
// Without flexibleStrings this degrades Analyze to config-only.
func TestAnalyzeOutput_FlexibleTechStack(t *testing.T) {
	input := "```json\n" + `{
  "endpoints": [],
  "pages": [],
  "tech_stack": [
    {"language": "go", "confidence": 1.0},
    {"build_tool": "make", "confidence": 0.95},
    {"formatter": "gofmt", "inferred": true, "confidence": 0.8}
  ]
}` + "\n```"

	var out AnalyzeOutput
	require.NoError(t, ai.ParseStructuredOutput(input, &out))
	assert.Equal(t, []string{"go", "make", "gofmt"}, []string(out.TechStack))
}

// TestAnalyzeOutput_FlatTechStack ensures the documented string-array shape
// (what the prompt asks for) still parses.
func TestAnalyzeOutput_FlatTechStack(t *testing.T) {
	input := `{"endpoints":[],"pages":[],"tech_stack":["go","make","git"]}`
	var out AnalyzeOutput
	require.NoError(t, ai.ParseStructuredOutput(input, &out))
	assert.Equal(t, []string{"go", "make", "git"}, []string(out.TechStack))
}
