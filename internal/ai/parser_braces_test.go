package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// A '}' inside a string value must not be treated as the object's closing
// brace — naive brace-counting truncates the object here and phase 3 returns
// invalid JSON.
func TestFindJSONObjectBoundsHandlesBracesInStrings(t *testing.T) {
	input := `prefix {"a":"x}y","b":1} suffix`
	bounds := findJSONObjectBounds(input)
	require.NotNil(t, bounds)
	obj := input[bounds.start : bounds.end+1]
	var m map[string]json.RawMessage
	require.NoErrorf(t, json.Unmarshal([]byte(obj), &m), "extracted object was: %q", obj)
	_, ok := m["b"]
	require.True(t, ok, "field b must be inside the extracted object (not truncated at the in-string })")
}
