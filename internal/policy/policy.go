// Package policy validates actions before execution and detects anomalies in results.
package policy

import (
	"github.com/binoctal/cerberus/internal/types"
)

// ActionPolicy validates actions before execution.
// Returns nil if the action is allowed, or an error describing why it was denied.
type ActionPolicy interface {
	Validate(action types.TypedAction) error
}
