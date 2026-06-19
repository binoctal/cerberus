package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSteer_FallsBackOnActionUnmarshalError verifies that when the LLM returns
// a steer action whose envelope type resolves but whose payload is empty (a
// common schema mismatch with non-Claude models), steer falls back to a safe
// action instead of hard-failing. Previously this error bypassed the
// isParseError fallback (it lives in types.UnmarshalAction, not the driver),
// wasting every MaxSteerAttempts retry and failing the whole test case.
func TestSteer_FallsBackOnActionUnmarshalError(t *testing.T) {
	loop, _ := testLoop(t, map[string]string{
		"default": `{"reasoning":"x","action":{"type":"api_request"}}`,
	}, nil)

	tc := &TestCase{ID: "tc-fb", Name: "fallback", Target: "/t", Expectation: "e"}

	action, err := loop.steer(context.Background(), tc, nil, 1)

	require.NoError(t, err, "steer must not hard-fail on action unmarshal error")
	assert.NotNil(t, action, "steer must return a fallback action")
}
