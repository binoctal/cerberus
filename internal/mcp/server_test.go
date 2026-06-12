// internal/mcp/server_test.go
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupMCPServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))
	srv := NewServer(s, zap.NewNop())
	return srv, s
}

func TestServer_ListTools(t *testing.T) {
	srv, _ := setupMCPServer(t)
	tools := srv.listTools()
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	assert.True(t, names["cerberus_run"])
	assert.True(t, names["cerberus_status"])
	assert.True(t, names["cerberus_report"])
	assert.True(t, names["cerberus_decide"])
	assert.True(t, names["cerberus_cancel"])
}

func TestServer_HandleListTools(t *testing.T) {
	srv, _ := setupMCPServer(t)
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)
	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)
}

func TestServer_HandleCancelNonexistentSession(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_cancel",
		"arguments": map[string]any{"session_id": "nonexistent"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)
	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Nil(t, resp.Error)
}
