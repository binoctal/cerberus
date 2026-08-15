package agent

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// expectCloseHarness wires one connection through the executor against a fake
// server whose handler decides how the peer ends the connection.
func expectCloseHarness(t *testing.T, handler func(conn *websocket.Conn)) *WebSocketExecutor {
	t.Helper()
	url := newWSTestServer(t, handler)
	e := NewWebSocketExecutor(zap.NewNop(), nil)
	res := e.Execute(t.Context(), types.WSConnectAction{URL: url, ConnectionID: "c1"})
	require.True(t, res.Success(), "connect must succeed: %v", res.Summary())
	return e
}

func TestWSExpectClose_MatchesCode(t *testing.T) {
	e := expectCloseHarness(t, func(conn *websocket.Conn) {
		_ = conn.Close(1009, "too large")
	})
	res := e.Execute(t.Context(), types.WSExpectCloseAction{ConnectionID: "c1", Code: 1009, Timeout: 5})
	require.True(t, res.Success(), "matching close code must pass: %v", res.Summary())
	wr, ok := res.(types.WSResult)
	require.True(t, ok, "result is a WSResult")
	assert.Equal(t, 1009, wr.CloseCode)
}

func TestWSExpectClose_WrongCodeFails(t *testing.T) {
	e := expectCloseHarness(t, func(conn *websocket.Conn) {
		_ = conn.Close(1008, "policy")
	})
	res := e.Execute(t.Context(), types.WSExpectCloseAction{ConnectionID: "c1", Code: 1009, Timeout: 5})
	require.False(t, res.Success(), "code mismatch is a finding, not a pass")
	wr, ok := res.(types.WSResult)
	require.True(t, ok)
	assert.Equal(t, 1008, wr.CloseCode, "observed code must surface in the result")
}

func TestWSExpectClose_TimeoutWhenNoClose(t *testing.T) {
	e := expectCloseHarness(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for { // stay silent until the server tears down
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	})
	res := e.Execute(t.Context(), types.WSExpectCloseAction{ConnectionID: "c1", Code: 1009, Timeout: 1})
	require.False(t, res.Success(), "no close within the timeout must fail")
	wr, ok := res.(types.WSResult)
	require.True(t, ok)
	assert.Contains(t, wr.Err, "no close")
}

func TestWSExpectClose_UnknownConnection(t *testing.T) {
	e := NewWebSocketExecutor(zap.NewNop(), nil)
	res := e.Execute(t.Context(), types.WSExpectCloseAction{ConnectionID: "ghost", Code: 1009, Timeout: 1})
	require.False(t, res.Success())
	wr, ok := res.(types.WSResult)
	require.True(t, ok)
	assert.Contains(t, wr.Err, "unknown connection_id")
}
