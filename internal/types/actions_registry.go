package types

import (
	"encoding/json"
	"fmt"
)

// actionFactory creates a new instance of an action type.
type actionFactory func() TypedAction

// unmarshalRegistry maps action types to their factory functions.
var unmarshalRegistry = map[ActionType]actionFactory{
	ActionAPIRequest:   func() TypedAction { return &HTTPAction{} },
	ActionNavigate:     func() TypedAction { return &NavigateAction{} },
	ActionWait:         func() TypedAction { return &WaitAction{} },
	ActionProcessExec:  func() TypedAction { return &ProcessExecAction{} },
	ActionProcessBuild: func() TypedAction { return &BuildAction{} },
	ActionFileRead:     func() TypedAction { return &FileReadAction{} },
	ActionFileWrite:    func() TypedAction { return &FileWriteAction{} },
	ActionFileExists:   func() TypedAction { return &FileExistsAction{} },
	ActionFileGlob:     func() TypedAction { return &FileGlobAction{} },
	ActionMCPCall:      func() TypedAction { return &MCPCallAction{} },
	ActionCodeAnalyze:  func() TypedAction { return &CodeAnalyzeAction{} },
	ActionCodeLint:     func() TypedAction { return &CodeLintAction{} },
	ActionCodeSymbols:  func() TypedAction { return &CodeSymbolsAction{} },
	ActionBrowserGoto:  func() TypedAction { return &BrowserGotoAction{} },
	ActionBrowserClick: func() TypedAction { return &BrowserClickAction{} },
	ActionBrowserFill:  func() TypedAction { return &BrowserFillAction{} },
	ActionBrowserEval:  func() TypedAction { return &BrowserEvalAction{} },
	ActionDBQuery:      func() TypedAction { return &DBQueryAction{} },
	ActionDBAssert:     func() TypedAction { return &DBAssertAction{} },
	ActionGraphQLQuery: func() TypedAction { return &GraphQLQueryAction{} },
	ActionWSConnect:    func() TypedAction { return &WSConnectAction{} },
	ActionWSSend:       func() TypedAction { return &WSSendAction{} },
}

// UnmarshalAction deserializes an ActionEnvelope into a concrete TypedAction.
func UnmarshalAction(envelope ActionEnvelope) (TypedAction, error) {
	factory, ok := unmarshalRegistry[envelope.Type]
	if !ok {
		return nil, fmt.Errorf("unknown action type: %s", envelope.Type)
	}
	action := factory()
	if err := json.Unmarshal(envelope.Raw, action); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", envelope.Type, err)
	}
	if err := action.Validate(); err != nil {
		return nil, err
	}
	return derefAction(action), nil
}

// MarshalAction serializes a TypedAction into an ActionEnvelope.
func MarshalAction(action TypedAction) (ActionEnvelope, error) {
	raw, err := json.Marshal(action)
	if err != nil {
		return ActionEnvelope{}, err
	}
	return ActionEnvelope{Type: action.GetActionType(), Raw: raw}, nil
}

// derefAction dereferences a pointer action to its value type.
// This is used after unmarshaling to convert pointer types to value types
// for consistent handling.
func derefAction(a TypedAction) TypedAction {
	// Try each action group
	if action, ok := derefHTTPActions(a); ok {
		return action
	}
	if action, ok := derefFileActions(a); ok {
		return action
	}
	if action, ok := derefCodeActions(a); ok {
		return action
	}
	if action, ok := derefBrowserActions(a); ok {
		return action
	}
	if action, ok := derefProcessDBActions(a); ok {
		return action
	}
	if action, ok := derefOtherActions(a); ok {
		return action
	}
	// Not a pointer type, return as-is
	return a
}
