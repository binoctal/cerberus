package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPAction_BodyTolerantTypes reproduces a real failure: a non-Claude
// model returned {"body": {...}} (object) but HTTPAction.Body is a string.
// Body must accept a string (used verbatim) or any JSON value (re-serialized
// to a JSON string) so the request body is preserved instead of dropped.
func TestHTTPAction_BodyTolerantTypes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"string body", `{"method":"POST","url":"/u","body":"plain text"}`, "plain text"},
		{"object body", `{"method":"POST","url":"/u","body":{"name":"test","n":3}}`, `{"name":"test","n":3}`},
		{"array body", `{"method":"POST","url":"/u","body":[1,2,3]}`, `[1,2,3]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a HTTPAction
			require.NoError(t, json.Unmarshal([]byte(tt.raw), &a), "input: %s", tt.raw)
			assert.Equal(t, tt.want, a.Body)
			require.NoError(t, a.Validate())
		})
	}

	// Other fields still decode through the alias path.
	t.Run("other fields preserved", func(t *testing.T) {
		var a HTTPAction
		require.NoError(t, json.Unmarshal([]byte(`{"method":"PUT","url":"/u","headers":{"a":"b"},"body":{"k":1},"timeout":7}`), &a))
		assert.Equal(t, "PUT", a.Method)
		assert.Equal(t, "/u", a.URL)
		assert.Equal(t, "b", a.Headers["a"])
		assert.Equal(t, `{"k":1}`, a.Body)
		assert.Equal(t, 7, a.Timeout)
	})
}
