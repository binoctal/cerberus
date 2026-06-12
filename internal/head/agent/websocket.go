package agent

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"

	"nhooyr.io/websocket"
)

// WebSocketExecutor handles WebSocket connect and send actions.
type WebSocketExecutor struct {
	logger *zap.Logger
}

// NewWebSocketExecutor creates a WebSocket executor.
func NewWebSocketExecutor(logger *zap.Logger) *WebSocketExecutor {
	return &WebSocketExecutor{logger: logger}
}

// Execute dispatches WebSocket actions.
func (e *WebSocketExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	switch a := action.(type) {
	case types.WSConnectAction:
		return e.doConnect(ctx, a, start)
	case types.WSSendAction:
		return e.doSend(ctx, a, start)
	default:
		return types.ErrorResult{Err: fmt.Sprintf("ws executor: unsupported action %T", action)}
	}
}

func (e *WebSocketExecutor) doConnect(ctx context.Context, a types.WSConnectAction, start time.Time) types.ExecutorResult {
	opts := &websocket.DialOptions{}
	headers := http.Header{}
	for k, v := range a.Headers {
		headers.Set(k, v)
	}

	// Convert http(s) URLs to ws(s).
	wsURL := a.URL
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)

	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		return types.WSResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	// Read one message as a handshake confirmation.
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, msg, err := conn.Read(readCtx)
	if err != nil {
		// Connection established but no immediate message — still OK.
		return types.WSResult{OK: true, URL: a.URL, Latency: time.Since(start)}
	}

	return types.WSResult{
		OK:       true,
		URL:      a.URL,
		Messages: []string{string(msg)},
		Latency:  time.Since(start),
	}
}

func (e *WebSocketExecutor) doSend(ctx context.Context, a types.WSSendAction, start time.Time) types.ExecutorResult {
	wsURL := a.URL
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return types.WSResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := conn.Write(writeCtx, websocket.MessageText, []byte(a.Message)); err != nil {
		return types.WSResult{OK: false, URL: a.URL, Err: fmt.Sprintf("write: %v", err), Latency: time.Since(start)}
	}

	// Read response.
	readCtx, readCancel := context.WithTimeout(ctx, 10*time.Second)
	defer readCancel()

	_, resp, err := conn.Read(readCtx)
	var messages []string
	if err == nil {
		messages = append(messages, string(resp))
	}

	return types.WSResult{
		OK:       true,
		URL:      a.URL,
		Messages: messages,
		Latency:  time.Since(start),
	}
}
