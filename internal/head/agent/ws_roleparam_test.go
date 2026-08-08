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
	assert.Equal(t, "bridge", resolveRoleParamValue("bridge", nil))
	assert.Equal(t, "", resolveRoleParamValue("", nil))
}

// TestResolveRoleParamValue_UUIDSentinelGenerated: the {{uuid}} sentinel is
// replaced with a valid, per-dial-distinct uuid — the fix that lets a protocol
// declare a dynamic identifier (e.g. bridge deviceId) without a literal.
func TestResolveRoleParamValue_UUIDSentinelGenerated(t *testing.T) {
	got := resolveRoleParamValue("{{uuid}}", nil)
	assert.NotEqual(t, "{{uuid}}", got, "sentinel must be replaced")
	_, err := uuid.Parse(got)
	assert.NoError(t, err, "generated value must be a valid uuid")
	assert.NotEqual(t, got, resolveRoleParamValue("{{uuid}}", nil), "each dial generates a fresh uuid")
}

// TestResolveRoleParamValue_PathParamTemplate: a "{name}" placeholder resolves
// from the resolved actor's captured path params (provisioning); the uuid
// sentinel still yields a fresh uuid; an unknown placeholder (not a captured
// param) and a plain literal are returned unchanged.
func TestResolveRoleParamValue_PathParamTemplate(t *testing.T) {
	params := map[string]string{"deviceId": "device_abc"}
	assert.Equal(t, "device_abc", resolveRoleParamValue("{deviceId}", params))
	assert.NotEqual(t, roleParamUUIDSentinel, resolveRoleParamValue(roleParamUUIDSentinel, params))
	assert.Equal(t, "{unknown}", resolveRoleParamValue("{unknown}", params))
	assert.Equal(t, "bridge", resolveRoleParamValue("bridge", params))
}
