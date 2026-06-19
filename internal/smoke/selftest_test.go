package smoke

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSelfTest(t *testing.T) {
	res, err := RunSelfTest(context.Background())
	require.NoError(t, err)
	assert.True(t, res.OK)
	assert.NotEmpty(t, res.Checks)
}
