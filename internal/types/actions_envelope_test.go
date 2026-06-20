package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActionEnvelope_TolerantShapes verifies the envelope decodes the JSON shapes
// real LLMs actually emit. The steer prompt documents a "payload" wrapper, but
// non-Claude models frequently flatten the action fields next to "type" instead.
// All three shapes must yield a populated Raw so UnmarshalAction succeeds.
func TestActionEnvelope_TolerantShapes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "payload wrapper (prompt schema)",
			raw:  `{"type":"api_request","payload":{"method":"GET","url":"http://localhost:8080/api/health"}}`,
		},
		{
			name: "flat fields next to type (GLM-style)",
			raw:  `{"type":"api_request","method":"GET","url":"http://localhost:8080/api/health"}`,
		},
		{
			name: "action wrapper (legacy)",
			raw:  `{"type":"api_request","action":{"method":"GET","url":"http://localhost:8080/api/health"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var env ActionEnvelope
			require.NoError(t, json.Unmarshal([]byte(tt.raw), &env), "decode envelope")
			assert.Equal(t, ActionAPIRequest, env.Type)
			require.NotEmpty(t, env.Raw, "Raw must be populated so UnmarshalAction does not hit empty input")

			action, err := UnmarshalAction(env)
			require.NoError(t, err, "UnmarshalAction must succeed")

			httpAct, ok := action.(HTTPAction)
			require.True(t, ok, "expected HTTPAction, got %T", action)
			assert.Equal(t, "GET", httpAct.Method)
			assert.Equal(t, "http://localhost:8080/api/health", httpAct.URL)
		})
	}
}
