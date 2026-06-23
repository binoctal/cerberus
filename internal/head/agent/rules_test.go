package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

func TestRuleEngineMatch_APIGet(t *testing.T) {
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "https://api.example.com"}}, nil, ".")
	tc := TestCase{
		ID:     "t1",
		Target: "/api/v1/users",
		Method: "GET",
	}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	httpAct, isHTTP := action.(types.HTTPAction)
	assert.True(t, isHTTP)
	assert.Equal(t, "GET", httpAct.Method)
	assert.Equal(t, "https://api.example.com/api/v1/users", httpAct.URL)
}

func TestRuleEngineMatch_APIPost(t *testing.T) {
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "https://api.example.com"}}, nil, ".")
	tc := TestCase{
		ID:     "t2",
		Target: "/api/v1/users",
		Method: "POST",
	}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	httpAct := action.(types.HTTPAction)
	assert.Equal(t, "POST", httpAct.Method)
}

func TestRuleEngineMatch_APIWithActors(t *testing.T) {
	actors := []project.Actor{
		{Name: "admin", Credentials: project.CredentialRef{Email: "admin@test.com", Password: "secret"}},
	}
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "https://api.example.com"}}, actors, ".")
	tc := TestCase{ID: "t3", Target: "/admin/users", Method: "GET"}

	action, ok := engine.Match(tc)
	assert.True(t, ok)
	httpAct := action.(types.HTTPAction)
	assert.Equal(t, "admin@test.com", httpAct.Headers["X-Test-User"])
}

func TestRuleEngineMatch_Navigate(t *testing.T) {
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "https://example.com"}}, nil, ".")
	tc := TestCase{
		ID:     "t4",
		Target: "/dashboard",
		Action: "navigate",
	}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	navAct, isNav := action.(types.NavigateAction)
	assert.True(t, isNav)
	assert.Equal(t, "https://example.com/dashboard", navAct.URL)
}

func TestRuleEngineMatch_FullURL(t *testing.T) {
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "https://example.com"}}, nil, ".")
	tc := TestCase{
		ID:     "t5",
		Target: "https://other.example.com/api/health",
	}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	navAct, isNav := action.(types.NavigateAction)
	assert.True(t, isNav)
	assert.Equal(t, "https://other.example.com/api/health", navAct.URL)
}

func TestRuleEngineMatch_FullURLWithMethod(t *testing.T) {
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "https://example.com"}}, nil, ".")
	tc := TestCase{
		ID:     "t6",
		Target: "https://api.example.com/v1/data",
		Method: "POST",
	}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	httpAct, isHTTP := action.(types.HTTPAction)
	assert.True(t, isHTTP)
	assert.Equal(t, "POST", httpAct.Method)
}

func TestRuleEngineMatch_NoMatch(t *testing.T) {
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "https://example.com"}}, nil, ".")
	tc := TestCase{
		ID:     "t7",
		Target: "verify login flow works correctly",
	}
	_, ok := engine.Match(tc)
	assert.False(t, ok)
}

func TestRuleEngineMatch_NoMatchNoMethod(t *testing.T) {
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "https://example.com"}}, nil, ".")
	tc := TestCase{
		ID:     "t8",
		Target: "/some/path",
	}
	_, ok := engine.Match(tc)
	assert.False(t, ok)
}

func TestRuleEngineMatch_TrailingSlash(t *testing.T) {
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "https://api.example.com/"}}, nil, ".")
	tc := TestCase{ID: "t9", Target: "/v1/users", Method: "GET"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	httpAct := action.(types.HTTPAction)
	assert.Equal(t, "https://api.example.com/v1/users", httpAct.URL)
}

// --- Non-HTTP rule tests ---

func TestRuleEngineMatch_ProcessExec(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, "/project")
	tc := TestCase{ID: "e1", Target: "go test ./...", Action: "process_exec"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	procAct, isProc := action.(types.ProcessExecAction)
	assert.True(t, isProc)
	// Command is split from the target so the policy allowlist (keyed on the
	// executable) can match: "go test ./..." -> Command="go", Args=["test","./..."].
	assert.Equal(t, "go", procAct.Command)
	assert.Equal(t, []string{"test", "./..."}, procAct.Args)
	assert.Equal(t, "/project", procAct.WorkDir)
}

func TestRuleEngineMatch_ProcessBuild(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, "/project")
	tc := TestCase{ID: "e2", Target: "go build ./...", Action: "process_build"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	buildAct, isBuild := action.(types.BuildAction)
	assert.True(t, isBuild)
	assert.Equal(t, types.ActionProcessBuild, buildAct.GetActionType())
	assert.Equal(t, "go", buildAct.Command)
	assert.Equal(t, []string{"build", "./..."}, buildAct.Args)
}

func TestRuleEngineMatch_CodeAnalyze(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, "/project")
	tc := TestCase{ID: "e3", Target: "/project", Action: "code_analyze"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	codeAct, isCode := action.(types.CodeAnalyzeAction)
	assert.True(t, isCode)
	assert.Equal(t, "/project", codeAct.TargetPath)
}

func TestRuleEngineMatch_CodeLint(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, "/project")
	tc := TestCase{ID: "e4", Target: "/project", Action: "code_lint"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	codeAct := action.(types.CodeLintAction)
	assert.Equal(t, "/project", codeAct.TargetPath)
}

func TestRuleEngineMatch_CodeSymbols(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, "/project")
	tc := TestCase{ID: "e5", Target: "/project", Action: "code_symbols"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	codeAct := action.(types.CodeSymbolsAction)
	assert.Equal(t, "/project", codeAct.TargetPath)
}

func TestRuleEngineMatch_FileRead(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, ".")
	tc := TestCase{ID: "e6", Target: "/etc/hosts", Action: "file_read"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	fileAct, isFile := action.(types.FileReadAction)
	assert.True(t, isFile)
	assert.Equal(t, "/etc/hosts", fileAct.Path)
}

func TestRuleEngineMatch_FileWrite(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, ".")
	tc := TestCase{ID: "e7", Target: "output.txt", Action: "file_write"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	fileAct := action.(types.FileWriteAction)
	assert.Equal(t, "output.txt", fileAct.Path)
}

func TestRuleEngineMatch_FileExists(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, ".")
	tc := TestCase{ID: "e8", Target: "go.mod", Action: "file_exists"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	fileAct := action.(types.FileExistsAction)
	assert.Equal(t, "go.mod", fileAct.Path)
}

func TestRuleEngineMatch_FileGlob(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, ".")
	tc := TestCase{ID: "e9", Target: "**/*.go", Action: "file_glob"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	globAct := action.(types.FileGlobAction)
	assert.Equal(t, "**/*.go", globAct.Pattern)
}

func TestRuleEngineMatch_Wait(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, ".")
	tc := TestCase{ID: "e10", Target: "5s", Action: "wait"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	waitAct, isWait := action.(types.WaitAction)
	assert.True(t, isWait)
	assert.Equal(t, "5s", waitAct.Duration)
}

func TestRuleEngineMatch_MCPCall(t *testing.T) {
	engine := NewRuleEngine([]project.Service{}, nil, ".")
	tc := TestCase{ID: "e11", Target: "filesystem", Method: "tools/call", Action: "mcp_call"}
	action, ok := engine.Match(tc)
	assert.True(t, ok)
	mcpAct, isMCP := action.(types.MCPCallAction)
	assert.True(t, isMCP)
	assert.Equal(t, "filesystem", mcpAct.Server)
	assert.Equal(t, "tools/call", mcpAct.Method)
}

func TestRuleEngine_RoutesByService(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://gw:8081"},
		{Name: "admin", URL: "http://admin:8086"},
	}
	engine := NewRuleEngine(services, nil, ".")

	action, ok := engine.Match(TestCase{
		Target: "/v1/chat", Method: "POST", Service: "admin",
	})
	require.True(t, ok)
	httpAct, ok := action.(types.HTTPAction)
	require.True(t, ok)
	assert.Equal(t, "http://admin:8086/v1/chat", httpAct.URL)
}

func TestRuleEngine_FallsBackToFirstService(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://gw:8081"},
		{Name: "admin", URL: "http://admin:8086"},
	}
	engine := NewRuleEngine(services, nil, ".")

	action, ok := engine.Match(TestCase{Target: "/v1/chat", Method: "POST"}) // Service empty
	require.True(t, ok)
	assert.Equal(t, "http://gw:8081/v1/chat", action.(types.HTTPAction).URL)
}
