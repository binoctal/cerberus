package agent

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestResolveRoleParamValue_LiteralUntouched: a literal role param value passes
// through unchanged (including the empty string, which the dial still sends as
// ?key= — that is the caller's responsibility, not the resolver's).
func TestResolveRoleParamValue_LiteralUntouched(t *testing.T) {
	assert.Equal(t, "bridge", resolveRoleParamValue("bridge"))
	assert.Equal(t, "", resolveRoleParamValue(""))
}

// TestResolveRoleParamValue_UUIDSentinelGenerated: the {{uuid}} sentinel is
// replaced with a valid, per-dial-distinct uuid — the fix that lets a protocol
// declare a dynamic identifier (e.g. bridge deviceId) without a literal.
func TestResolveRoleParamValue_UUIDSentinelGenerated(t *testing.T) {
	got := resolveRoleParamValue("{{uuid}}")
	assert.NotEqual(t, "{{uuid}}", got, "sentinel must be replaced")
	_, err := uuid.Parse(got)
	assert.NoError(t, err, "generated value must be a valid uuid")
	assert.NotEqual(t, got, resolveRoleParamValue("{{uuid}}"), "each dial generates a fresh uuid")
}
