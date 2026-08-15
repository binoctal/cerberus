package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/session"
)

// TestMapRunExitError pins the run exit-code mapping: the claims gate sentinel
// (3) stays recognizable through the wrapping RunE adds, and everything else
// falls through to the generic error path (0 → cobra's exit 1).
func TestMapRunExitError(t *testing.T) {
	assert.Equal(t, 3, mapRunExitError(session.ErrClaimsGate))
	assert.Equal(t, 3, mapRunExitError(fmt.Errorf("session run: %w", session.ErrClaimsGate)))
	assert.Equal(t, 3, mapRunExitError(fmt.Errorf("session resume: %w", session.ErrClaimsGate)))
	assert.Equal(t, 0, mapRunExitError(errors.New("agent execute: boom")))
	assert.Equal(t, 0, mapRunExitError(nil))
}
