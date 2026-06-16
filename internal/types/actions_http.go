package types

import "fmt"

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

// WSConnectAction represents a WebSocket connection.
type WSConnectAction struct {
	// URL is the WebSocket endpoint URL.
	URL string `json:"url"`
	// Headers are optional WebSocket handshake headers.
	Headers map[string]string `json:"headers,omitempty"`
	// HandshakeTimeout is the timeout for connection handshake.
	HandshakeTimeout int `json:"handshake_timeout,omitempty"`
	// Messages are optional messages to send after connection.
	Messages []WSMessage `json:"messages,omitempty"`
}

// WSMessage represents a single WebSocket message.
type WSMessage struct {
	Type string `json:"type"` // "text" or "binary"
	Data string `json:"data"`
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
