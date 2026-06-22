package autotest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A wrong type or nil driver must fail fast, not silently leave driver=nil
// and panic later in Generate on a nil-pointer dereference.
func TestNewPythonTestGeneratorRejectsBadDriver(t *testing.T) {
	assert.Panics(t, func() { NewPythonTestGenerator("not-a-driver") })
	assert.Panics(t, func() { NewPythonTestGenerator(nil) })
}
