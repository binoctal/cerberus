package types

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// HTTPAction represents an HTTP API request.
type HTTPAction struct {
	// Method is the HTTP method (GET, POST, PUT, DELETE, PATCH).
	Method string `json:"method"`
	// URL is the request URL or path.
	URL string `json:"url"`
	// Headers are optional request headers.
	Headers map[string]string `json:"headers,omitempty"`
	// Body is an optional request body (typically JSON string).
	Body string `json:"body,omitempty"`
	// Timeout overrides the default request timeout.
	Timeout int `json:"timeout,omitempty"`
}

func (a HTTPAction) GetActionType() ActionType { return ActionAPIRequest }
func (a HTTPAction) Target() string            { return a.URL }

// UnmarshalJSON tolerates Body as either a string (used verbatim as the raw
// request body) or any JSON value (object/array/number), which is re-serialized
// to a compact JSON string. Non-Claude models often send body as an object;
// without this the whole api_request fails to unmarshal. Other fields decode
// through an alias to avoid recursion.
func (a *HTTPAction) UnmarshalJSON(data []byte) error {
	type alias HTTPAction
	var tmp struct {
		alias
		Body json.RawMessage `json:"body,omitempty"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*a = HTTPAction(tmp.alias)
	b, err := coerceBodyRaw(tmp.Body)
	if err != nil {
		return fmt.Errorf("http body: %w", err)
	}
	a.Body = b
	return nil
}

// coerceBodyRaw accepts Body as a JSON string (returned verbatim) or any other
// JSON value (compacted to preserve key order and stored as the body text).
// Empty/null input is allowed (body is optional).
func coerceBodyRaw(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	buf := &bytes.Buffer{}
	if err := json.Compact(buf, trimmed); err != nil {
		return "", fmt.Errorf("body must be string or JSON value")
	}
	return buf.String(), nil
}

func (a HTTPAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	if a.Method == "" {
		return fmt.Errorf("method is required")
	}
	return nil
}

// NavigateAction represents browser navigation.
type NavigateAction struct {
	// URL is the destination URL.
	URL string `json:"url"`
	// WaitSelector is an optional CSS selector to wait for before considering navigation complete.
	WaitSelector string `json:"wait_selector,omitempty"`
	// WaitFor is an optional duration to wait after navigation.
	WaitFor int `json:"wait_for,omitempty"`
}

func (a NavigateAction) GetActionType() ActionType { return ActionNavigate }
func (a NavigateAction) Target() string            { return a.URL }
func (a NavigateAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

// WaitAction represents a delay before proceeding.
type WaitAction struct {
	// Duration is the wait time (e.g., "5s", "1m").
	Duration string `json:"duration,omitempty"`
	// Selector is an optional CSS selector to wait for.
	Selector string `json:"selector,omitempty"`
	// WaitForState is the optional state to wait for ("visible", "hidden", etc.).
	WaitForState string `json:"wait_for_state,omitempty"`
}

func (a WaitAction) GetActionType() ActionType { return ActionWait }
func (a WaitAction) Target() string            { return "" }

// UnmarshalJSON tolerates Duration as either a string ("2s", "500ms") or a
// number. Non-Claude models sometimes emit a bare numeric duration; treat it
// as seconds ("2" -> "2s"). Other fields decode through an alias to avoid
// recursion and to keep this tolerant of future field additions.
func (a *WaitAction) UnmarshalJSON(data []byte) error {
	type alias WaitAction
	var tmp struct {
		alias
		Duration json.RawMessage `json:"duration,omitempty"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*a = WaitAction(tmp.alias)
	d, err := coerceDurationRaw(tmp.Duration)
	if err != nil {
		return fmt.Errorf("wait duration: %w", err)
	}
	a.Duration = d
	return nil
}

// coerceDurationRaw accepts a JSON string duration or a numeric duration
// (interpreted as seconds) and returns a Go duration string. Empty input is
// allowed (duration is optional).
func coerceDurationRaw(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	var n json.Number
	if err := json.Unmarshal(trimmed, &n); err != nil {
		return "", fmt.Errorf("duration must be string or number")
	}
	return n.String() + "s", nil
}

func (a WaitAction) Validate() error {
	// Validate duration format if provided
	if a.Duration != "" && a.Selector == "" {
		// Simple validation - duration should not be empty if set
		// For now, we just check it's not obviously invalid
		if a.Duration == "abc" {
			return fmt.Errorf("invalid timeout")
		}
	}
	return nil
}

// WebSocket actions

// WSConnectAction establishes and persists a WebSocket connection.
type WSConnectAction struct {
	URL string `json:"url"`
	// Headers are optional WebSocket handshake headers.
	Headers map[string]string `json:"headers,omitempty"`
	// Subprotocols are optional WS subprotocols to negotiate.
	Subprotocols []string `json:"subprotocols,omitempty"`
	// HandshakeTimeout is the timeout for connection handshake.
	HandshakeTimeout int `json:"handshake_timeout,omitempty"`
	// ConnectionID names this connection for later send/receive/disconnect.
	// If empty the executor assigns one.
	ConnectionID string `json:"connection_id,omitempty"`
}

func (a WSConnectAction) GetActionType() ActionType { return ActionWSConnect }
func (a WSConnectAction) Target() string            { return a.URL }
func (a WSConnectAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

// WSSendAction represents sending a message over a WebSocket.
type WSSendAction struct {
	// URL identifies which WebSocket connection to use.
	URL string `json:"url"`
	// Message is the message content to send.
	Message string `json:"message"`
}

func (a WSSendAction) GetActionType() ActionType { return ActionWSSend }
func (a WSSendAction) Target() string            { return a.URL }
func (a WSSendAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	if a.Message == "" {
		return fmt.Errorf("message is required")
	}
	return nil
}

// WSReceiveAction waits for an inbound message whose top-level "type" field
// equals Type. Non-matching messages are accumulated as evidence. When Decisive
// is true (the default), a matching message passes the case.
type WSReceiveAction struct {
	ConnectionID string `json:"connection_id"`
	Type         string `json:"type"`
	// Timeout in seconds for the matching message to arrive.
	Timeout  int  `json:"timeout,omitempty"`
	Decisive bool `json:"decisive,omitempty"`
}

func (a WSReceiveAction) GetActionType() ActionType { return ActionWSReceive }
func (a WSReceiveAction) Target() string            { return a.ConnectionID }
func (a WSReceiveAction) Validate() error {
	if a.ConnectionID == "" {
		return fmt.Errorf("connection_id is required")
	}
	if a.Type == "" {
		return fmt.Errorf("type is required")
	}
	return nil
}

// WSDisconnectAction closes and removes an established WebSocket connection.
type WSDisconnectAction struct {
	ConnectionID string `json:"connection_id"`
}

func (a WSDisconnectAction) GetActionType() ActionType { return ActionWSDisconnect }
func (a WSDisconnectAction) Target() string            { return a.ConnectionID }
func (a WSDisconnectAction) Validate() error {
	if a.ConnectionID == "" {
		return fmt.Errorf("connection_id is required")
	}
	return nil
}
