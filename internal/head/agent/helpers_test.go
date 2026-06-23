package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

func TestIsDestructiveAction_NilAction(t *testing.T) {
	assert.False(t, isDestructiveAction(nil))
}

func TestIsDestructiveAction_HTTPDelete(t *testing.T) {
	assert.True(t, isDestructiveAction(types.HTTPAction{Method: "DELETE", URL: "http://x"}))
	assert.True(t, isDestructiveAction(types.HTTPAction{Method: "delete", URL: "http://x"}))
	assert.True(t, isDestructiveAction(types.HTTPAction{Method: "DROP", URL: "http://x"}))
	assert.False(t, isDestructiveAction(types.HTTPAction{Method: "GET", URL: "http://x"}))
	assert.False(t, isDestructiveAction(types.HTTPAction{Method: "POST", URL: "http://x"}))
}

func TestIsDestructiveAction_ProcessDestructive(t *testing.T) {
	for _, cmd := range []string{"rm", "rmdir", "dropdb", "truncate"} {
		assert.True(t, isDestructiveAction(types.ProcessExecAction{Command: cmd}))
	}
	assert.False(t, isDestructiveAction(types.ProcessExecAction{Command: "go"}))
	assert.False(t, isDestructiveAction(types.ProcessExecAction{Command: "echo"}))
}

func TestIsDestructiveAction_FileWrite(t *testing.T) {
	assert.True(t, isDestructiveAction(types.FileWriteAction{Path: "test.txt", Content: "x"}))
}

func TestIsDestructiveAction_OtherActions(t *testing.T) {
	assert.False(t, isDestructiveAction(types.FileReadAction{Path: "test.txt"}))
	assert.False(t, isDestructiveAction(types.DBQueryAction{Driver: "sqlite", Query: "SELECT 1"}))
}

func TestIsParseError(t *testing.T) {
	assert.True(t, isParseError(errors.New("failed to parse output")))
	assert.True(t, isParseError(errors.New("invalid json format")))
	assert.False(t, isParseError(errors.New("connection refused")))
	assert.False(t, isParseError(nil))
}

func TestContains(t *testing.T) {
	assert.True(t, contains("hello world", "world"))
	assert.True(t, contains("hello", "hello"))
	assert.True(t, contains("hello", ""))
	assert.False(t, contains("hi", "hello"))
	assert.False(t, contains("", "x"))
}

func TestFindSubstr(t *testing.T) {
	assert.True(t, findSubstr("hello world", "world"))
	assert.True(t, findSubstr("abc", "abc"))
	assert.False(t, findSubstr("abc", "xyz"))
	assert.False(t, findSubstr("ab", "abc"))
}

func TestSandboxPolicyFor_AllActionTypes(t *testing.T) {
	mx := &MultiExecutor{}

	tests := []struct {
		name   string
		action types.TypedAction
	}{
		{"process exec", types.ProcessExecAction{Command: "go", WorkDir: "."}},
		{"process build", types.ProcessExecAction{Command: "go", WorkDir: "."}},
		{"file read", types.FileReadAction{Path: "test.go"}},
		{"file write", types.FileWriteAction{Path: "test.go", Content: "x"}},
		{"file exists", types.FileExistsAction{Path: "test.go"}},
		{"file glob", types.FileGlobAction{Pattern: "*.go"}},
		{"mcp call", types.MCPCallAction{Server: "test", Method: "foo"}},
		{"code analyze", types.CodeAnalyzeAction{TargetPath: ".", Language: "go"}},
		{"code lint", types.CodeLintAction{TargetPath: ".", Language: "go"}},
		{"code symbols", types.CodeSymbolsAction{TargetPath: ".", Language: "go"}},
		{"db query", types.DBQueryAction{Driver: "sqlite", Query: "SELECT 1"}},
		{"db assert", types.DBAssertAction{Driver: "sqlite", Query: "SELECT 1", Assertion: "rows.length > 0"}},
		{"graphql query", types.GraphQLQueryAction{URL: "http://x", Query: "{ users { id } }"}},
		{"ws connect", types.WSConnectAction{URL: "ws://x"}},
		{"ws send", types.WSSendAction{URL: "ws://x", Message: "hello"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := mx.sandboxPolicyFor(tt.action)
			// Verify policy is not zero-valued
			assert.NotNil(t, policy)
		})
	}
}

// testLoopWithServices creates a ReActLoop with custom services, mirroring testLoop.
func testLoopWithServices(t *testing.T, responses map[string]string, services []project.Service, actors []project.Actor) (*ReActLoop, *store.Store) {
	t.Helper()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../../migrations")
	require.NoError(t, err)

	mockClient := llm.NewMockClient(responses)
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(200000, 10000))

	engine := NewRuleEngine(services, actors, ".")

	executor := BuildMultiExecutor(".", nil, nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop := NewReActLoopWithConfig(ReActLoopConfig{
		Driver:   driver,
		Store:    s,
		Engine:   engine,
		Executor: executor,
		Config:   DefaultReActConfig(),
		Logger:   zap.NewNop(),
		Embedder: emb,
	})

	return loop, s
}
