package types

import (
	"encoding/json"
)

// MarshalAction serializes a TypedAction into an ActionEnvelope.
//
// Sole caller: examiner/judge.go serializes the prior step's action into the
// judge-evidence prompt text. The reverse direction (UnmarshalAction) and the
// tolerant ActionEnvelope.UnmarshalJSON were deleted in S3 — Agent now sources
// actions from typed tool calls, so the LLM-drift decode path is obsolete.
func MarshalAction(action TypedAction) (ActionEnvelope, error) {
	raw, err := json.Marshal(action)
	if err != nil {
		return ActionEnvelope{}, err
	}
	return ActionEnvelope{Type: action.GetActionType(), Raw: raw}, nil
}
