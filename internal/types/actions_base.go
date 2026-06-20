package types

import (
	"encoding/json"
	"fmt"
)

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

// UnmarshalJSON decodes an action envelope tolerantly. Real-world LLMs emit the
// action payload under several shapes, and a strict decode left Raw empty
// (causing "unexpected end of JSON input" downstream) for any shape except the
// legacy "action" wrapper. We accept, in order of preference:
//   - "payload" wrapper: {"type":"X","payload":{...}}  (the shape the steer prompt documents)
//   - "action" wrapper:  {"type":"X","action":{...}}   (legacy / marshal round-trip)
//   - flat fields:       {"type":"X", ...fields...}    (common with non-Claude models)
func (e *ActionEnvelope) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}

	typeRaw, ok := probe["type"]
	if !ok {
		return fmt.Errorf("action envelope missing \"type\" field")
	}
	var t ActionType
	if err := json.Unmarshal(typeRaw, &t); err != nil {
		return fmt.Errorf("action envelope \"type\": %w", err)
	}
	e.Type = t

	// Prefer the prompt-documented "payload" key, then the legacy "action" key.
	for _, key := range []string{"payload", "action"} {
		if raw, ok := probe[key]; ok {
			e.Raw = raw
			return nil
		}
	}

	// Flat shape: rebuild an object from every field except "type" so the action
	// body can be unmarshaled into the concrete action struct as-is.
	delete(probe, "type")
	rebuilt, err := json.Marshal(probe)
	if err != nil {
		return fmt.Errorf("rebuild flat action payload: %w", err)
	}
	e.Raw = rebuilt
	return nil
}
