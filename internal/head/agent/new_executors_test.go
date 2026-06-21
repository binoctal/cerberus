package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// --- Database Executor Tests ---

func TestDatabaseExecutor_Query(t *testing.T) {
	exec := NewDatabaseExecutor(zap.NewNop())
	ctx := context.Background()

	result := exec.Execute(ctx, types.DBQueryAction{
		Driver: "sqlite",
		DSN:    ":memory:",
		Query:  "SELECT 1 AS val",
	})

	dbResult, ok := result.(types.DBResult)
	require.True(t, ok)
	assert.True(t, dbResult.OK)
	require.Len(t, dbResult.Rows, 1)
	assert.Equal(t, "1", fmt.Sprintf("%v", dbResult.Rows[0]["val"]))
}

func TestDatabaseExecutor_Assert(t *testing.T) {
	exec := NewDatabaseExecutor(zap.NewNop())
	ctx := context.Background()

	// Assertion passes.
	result := exec.Execute(ctx, types.DBAssertAction{
		Driver:    "sqlite",
		DSN:       ":memory:",
		Query:     "SELECT 5 AS count",
		Assertion: "count == 5",
	})
	dbResult := result.(types.DBResult)
	assert.True(t, dbResult.OK)
	assert.True(t, dbResult.AssertionPassed)

	// Assertion fails.
	result = exec.Execute(ctx, types.DBAssertAction{
		Driver:    "sqlite",
		DSN:       ":memory:",
		Query:     "SELECT 5 AS count",
		Assertion: "count == 0",
	})
	dbResult = result.(types.DBResult)
	assert.False(t, dbResult.OK)
	assert.False(t, dbResult.AssertionPassed)
}

func TestDatabaseExecutor_RowsLength(t *testing.T) {
	exec := NewDatabaseExecutor(zap.NewNop())
	ctx := context.Background()

	result := exec.Execute(ctx, types.DBAssertAction{
		Driver:    "sqlite",
		DSN:       ":memory:",
		Query:     "SELECT 1 UNION ALL SELECT 2",
		Assertion: "rows.length > 0",
	})
	dbResult := result.(types.DBResult)
	assert.True(t, dbResult.OK)
	assert.True(t, dbResult.AssertionPassed)
}

func TestDatabaseExecutor_InvalidDriver(t *testing.T) {
	exec := NewDatabaseExecutor(zap.NewNop())
	ctx := context.Background()

	result := exec.Execute(ctx, types.DBQueryAction{
		Driver: "unknown",
		DSN:    "",
		Query:  "SELECT 1",
	})
	dbResult := result.(types.DBResult)
	assert.False(t, dbResult.OK)
	assert.Contains(t, dbResult.Err, "unknown")
}

// --- GraphQL Executor Tests ---

func TestGraphQLExecutor_Query(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Contains(t, body["query"], "users")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"users":[{"name":"Alice"}]}}`))
	}))
	defer srv.Close()

	exec := NewGraphQLExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.GraphQLQueryAction{
		URL:   srv.URL,
		Query: "{ users { name } }",
	})

	gqlResult := result.(types.GraphQLResult)
	assert.True(t, gqlResult.OK)
	require.NotNil(t, gqlResult.Data["users"])
}

func TestGraphQLExecutor_WithVariables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "admin", body["variables"].(map[string]any)["role"])

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()

	exec := NewGraphQLExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.GraphQLQueryAction{
		URL:       srv.URL,
		Query:     "query($role: String!) { users(role: $role) { id } }",
		Variables: map[string]any{"role": "admin"},
	})

	gqlResult := result.(types.GraphQLResult)
	assert.True(t, gqlResult.OK)
}

func TestGraphQLExecutor_GraphQLErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"Not found"}]}`))
	}))
	defer srv.Close()

	exec := NewGraphQLExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.GraphQLQueryAction{
		URL:   srv.URL,
		Query: "{ nonexistent }",
	})

	gqlResult := result.(types.GraphQLResult)
	assert.False(t, gqlResult.OK)
	assert.Len(t, gqlResult.Errors, 1)
}

// --- WebSocket Executor Tests ---

func TestWebSocketExecutor_UnsupportedAction(t *testing.T) {
	exec := NewWebSocketExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{URL: "http://test"})
	errResult, ok := result.(types.ErrorResult)
	require.True(t, ok)
	assert.Contains(t, errResult.Err, "unsupported action")
}

// --- Plugin Registration Tests ---

func TestBuiltinPluginsIncludesNewExecutors(t *testing.T) {
	plugins := BuiltinPluginsWithSandbox(".", nil, nil, nil, zap.NewNop())
	names := make(map[string]bool)
	for _, p := range plugins {
		names[p.Name()] = true
	}

	assert.True(t, names["database"], "database plugin should be registered")
	assert.True(t, names["graphql"], "graphql plugin should be registered")
	assert.True(t, names["websocket"], "websocket plugin should be registered")
}

func TestDBActionSerialization(t *testing.T) {
	original := types.DBQueryAction{
		Driver: "sqlite",
		DSN:    ":memory:",
		Query:  "SELECT 1",
		Args:   []any{1, "test"},
	}

	envelope, err := types.MarshalAction(original)
	require.NoError(t, err)
	assert.Equal(t, types.ActionDBQuery, envelope.Type)

	action, err := types.UnmarshalAction(envelope)
	require.NoError(t, err)
	decoded, ok := action.(types.DBQueryAction)
	require.True(t, ok)
	assert.Equal(t, "sqlite", decoded.Driver)
	assert.Equal(t, "SELECT 1", decoded.Query)
}

func TestGraphQLActionSerialization(t *testing.T) {
	original := types.GraphQLQueryAction{
		URL:           "http://api.test/graphql",
		Query:         "{ users { id } }",
		Variables:     map[string]any{"limit": 10},
		OperationName: "GetUsers",
	}

	envelope, err := types.MarshalAction(original)
	require.NoError(t, err)
	assert.Equal(t, types.ActionGraphQLQuery, envelope.Type)

	action, err := types.UnmarshalAction(envelope)
	require.NoError(t, err)
	decoded, ok := action.(types.GraphQLQueryAction)
	require.True(t, ok)
	assert.Equal(t, "GetUsers", decoded.OperationName)
}

func TestWSActionSerialization(t *testing.T) {
	for _, original := range []types.TypedAction{
		types.WSConnectAction{URL: "ws://localhost:8080/ws"},
		types.WSSendAction{URL: "ws://localhost:8080/ws", Message: "hello"},
	} {
		envelope, err := types.MarshalAction(original)
		require.NoError(t, err)

		action, err := types.UnmarshalAction(envelope)
		require.NoError(t, err)
		assert.Equal(t, original.GetActionType(), action.GetActionType())
	}
}
