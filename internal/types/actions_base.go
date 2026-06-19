package types

import "encoding/json"

// TypedAction defines the interface that all concrete action types must implement.
type TypedAction interface {
	// GetActionType returns the action type identifier.
	GetActionType() ActionType
	// Target returns the primary target of this action (URL, file path, command, etc.).
	Target() string
	// Validate checks if the action fields are valid.
	Validate() error
}

// ActionEnvelope wraps a serialized action with its type metadata.
// Used for JSON marshaling/unmarshaling of polymorphic action types.
type ActionEnvelope struct {
	Type ActionType      `json:"type"`
	Raw  json.RawMessage `json:"action"`
}
