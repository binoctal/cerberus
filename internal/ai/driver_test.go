package ai

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenBudget(t *testing.T) {
	b := NewTokenBudget(100000, 10000)
	assert.Equal(t, 100000, b.SessionTotal)
	assert.Equal(t, 100000, b.Remaining())

	b.Record(30000)
	assert.Equal(t, 70000, b.Remaining())
	assert.False(t, b.Exhausted())

	b.Record(70000)
	assert.True(t, b.Exhausted())
}

func TestTokenBudgetCanSpend(t *testing.T) {
	b := NewTokenBudget(100000, 10000)
	assert.True(t, b.CanSpend(5000))
	assert.True(t, b.CanSpend(10000))
	assert.False(t, b.CanSpend(10001))
}

func TestPromptBuilder(t *testing.T) {
	prompt := NewPrompt().
		System("You are a test judge.").
		Task("Evaluate this evidence: status code 200").
		Output("JSON with status and confidence fields").
		Build()

	assert.Contains(t, prompt, "You are a test judge.")
	assert.Contains(t, prompt, "Evaluate this evidence")
	assert.Contains(t, prompt, "JSON with status")
}

func TestContextInjection(t *testing.T) {
	entries := []ContextEntry{
		{Source: "memory", Content: "Last test found 500 error", Relevance: 0.9},
		{Source: "code", Content: "Endpoint: POST /api/v1/users", Relevance: 0.8},
	}
	ctx := BuildContext(entries)
	assert.Contains(t, ctx, "Last test found 500 error")
	assert.Contains(t, ctx, "POST /api/v1/users")
}

func TestParseStructuredOutput(t *testing.T) {
	type TestResult struct {
		Status     string  `json:"status"`
		Confidence float64 `json:"confidence"`
	}

	input := `Here is my analysis:
` + "```" + `json
{"status": "pass", "confidence": 0.95}
` + "```" + `
The test passed.`

	var result TestResult
	err := ParseStructuredOutput(input, &result)
	require.NoError(t, err)
	assert.Equal(t, "pass", result.Status)
	assert.InDelta(t, 0.95, result.Confidence, 0.01)
}

func TestParseStructuredOutputDirect(t *testing.T) {
	type Result struct{ Status string `json:"status"` }
	var r Result
	err := ParseStructuredOutput(`{"status":"fail"}`, &r)
	require.NoError(t, err)
	assert.Equal(t, "fail", r.Status)
}

func TestDriverDecide(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"verdict":"pass","confidence":0.9,"reasoning":"looks good"}`,
	})

	driver := NewDriver(mockClient, NewTokenBudget(200000, 10000))

	type Verdict struct {
		Verdict    string  `json:"verdict"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	var v Verdict
	err := driver.Decide(context.Background(),
		NewPrompt().System("judge").Task("evaluate").Output("JSON verdict").Build(),
		&v,
	)
	require.NoError(t, err)
	assert.Equal(t, "pass", v.Verdict)
	assert.Equal(t, "looks good", v.Reasoning)

	assert.Less(t, driver.Budget().Remaining(), 200000)
}
