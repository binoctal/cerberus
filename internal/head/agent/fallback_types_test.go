package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestCase_FallbackForRoundTrip(t *testing.T) {
	tc := TestCase{ID: "tc-001", FallbackFor: "tc-000"}
	b, err := json.Marshal(tc)
	require.NoError(t, err)
	var got TestCase
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "tc-000", got.FallbackFor, "FallbackFor round-trips")

	// omitempty: an empty FallbackFor is absent from the JSON.
	eb, err := json.Marshal(TestCase{ID: "tc-002"})
	require.NoError(t, err)
	assert.NotContains(t, string(eb), "fallback_for", "empty FallbackFor is omitted")
}

func TestStepResult_RecoveredZero(t *testing.T) {
	var r StepResult
	assert.False(t, r.Recovered, "Recovered zero value is false")
	r.Recovered = true
	assert.True(t, r.Recovered)
}
