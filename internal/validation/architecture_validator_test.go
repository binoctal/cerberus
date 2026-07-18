package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewArchitectureValidator(t *testing.T) {
	v := NewArchitectureValidator("/some/path")
	assert.NotNil(t, v)
}

// Validate runs the architecture analyzer over the project tree and returns a
// structured result. The analyzer tolerates an empty tree (existing architecture
// tests already drive it on a temp dir). We assert Validate's contract — no
// error, non-nil structured result, populated description — without pinning
// Passed, whose value depends on the analyzer's own findings rather than on
// Validate's wrapping logic.
func TestArchitectureValidator_ValidateEmptyDir(t *testing.T) {
	v := NewArchitectureValidator(t.TempDir())
	res, err := v.Validate()
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.NotEmpty(t, res.Description)
}
