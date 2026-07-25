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
//
// Kept (S4) as the marshal shape Examiner uses to render the prior step's
// action into judge-evidence prompt text (see types.MarshalAction). The
// tolerant UnmarshalJSON path that coerced three real-world LLM shapes
// (payload wrapper / action wrapper / flat fields) was deleted in S3: Agent
// now emits actions as typed tool calls assembled directly to TypedAction, so
// the LLM-drift decode path no longer exists.
type ActionEnvelope struct {
	Type ActionType      `json:"type"`
	Raw  json.RawMessage `json:"action"`
}
