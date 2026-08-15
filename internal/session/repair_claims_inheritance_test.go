package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// TestInheritClaims: repair-loop replacements and lazy fallbacks inherit the
// original case's claim bindings — a repaired case must not silently drop the
// promise it was proving (claims reconciliation would lose the evidence).
func TestInheritClaims(t *testing.T) {
	originals := []agent.TestCase{
		{ID: "A", Claims: []string{"schedule-real-cli", "ws-relay-messaging"}},
		{ID: "B", Claims: []string{"permission-approval"}},
		{ID: "C"}, // no claims bound
	}
	t.Run("replacement inherits", func(t *testing.T) {
		out := inheritClaims([]agent.TestCase{{ID: "A-r", Replaces: "A"}}, originals)
		assert.Equal(t, []string{"schedule-real-cli", "ws-relay-messaging"}, out[0].Claims)
	})
	t.Run("fallback inherits", func(t *testing.T) {
		out := inheritClaims([]agent.TestCase{{ID: "B'", FallbackFor: "B"}}, originals)
		assert.Equal(t, []string{"permission-approval"}, out[0].Claims)
	})
	t.Run("own claims kept", func(t *testing.T) {
		out := inheritClaims([]agent.TestCase{{ID: "A-r", Replaces: "A", Claims: []string{"explicit"}}}, originals)
		assert.Equal(t, []string{"explicit"}, out[0].Claims, "an explicit binding is never overwritten")
	})
	t.Run("original without claims stays empty", func(t *testing.T) {
		out := inheritClaims([]agent.TestCase{{ID: "C-r", Replaces: "C"}}, originals)
		assert.Empty(t, out[0].Claims)
	})
	t.Run("unknown original is untouched", func(t *testing.T) {
		out := inheritClaims([]agent.TestCase{{ID: "X-r", Replaces: "missing"}}, originals)
		assert.Empty(t, out[0].Claims)
	})
}
