package agent

import (
	"github.com/binoctal/cerberus/internal/types"
)

// --- Built-in executor plugin wrappers ---

type httpPlugin struct{ executor *HTTPExecutor }

func (p *httpPlugin) Name() string            { return "http" }
func (p *httpPlugin) Executor() TypedExecutor { return p.executor }
func (p *httpPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionAPIRequest, types.ActionNavigate}
}

type processPlugin struct{ executor *ProcessExecutor }

func (p *processPlugin) Name() string            { return "process" }
func (p *processPlugin) Executor() TypedExecutor { return p.executor }
func (p *processPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionProcessExec, types.ActionProcessBuild}
}

type filePlugin struct{ executor *FileExecutor }

func (p *filePlugin) Name() string            { return "file" }
func (p *filePlugin) Executor() TypedExecutor { return p.executor }
func (p *filePlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionFileRead, types.ActionFileWrite, types.ActionFileExists, types.ActionFileGlob}
}

type mcpPlugin struct{ executor *MCPExecutor }

func (p *mcpPlugin) Name() string            { return "mcp" }
func (p *mcpPlugin) Executor() TypedExecutor { return p.executor }
func (p *mcpPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionMCPCall}
}

type codePlugin struct{ executor *CodeExecutor }

func (p *codePlugin) Name() string            { return "code" }
func (p *codePlugin) Executor() TypedExecutor { return p.executor }
func (p *codePlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionCodeAnalyze, types.ActionCodeLint, types.ActionCodeSymbols}
}

type waitPlugin struct{ executor *WaitExecutor }

func (p *waitPlugin) Name() string            { return "wait" }
func (p *waitPlugin) Executor() TypedExecutor { return p.executor }
func (p *waitPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionWait}
}

type browserPlugin struct{ executor *BrowserExecutor }

func (p *browserPlugin) Name() string            { return "browser" }
func (p *browserPlugin) Executor() TypedExecutor { return p.executor }
func (p *browserPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionBrowserGoto, types.ActionBrowserClick,
		types.ActionBrowserFill, types.ActionBrowserEval}
}

type dbPlugin struct{ executor *DatabaseExecutor }

func (p *dbPlugin) Name() string            { return "database" }
func (p *dbPlugin) Executor() TypedExecutor { return p.executor }
func (p *dbPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionDBQuery, types.ActionDBAssert}
}

type graphqlPlugin struct{ executor *GraphQLExecutor }

func (p *graphqlPlugin) Name() string            { return "graphql" }
func (p *graphqlPlugin) Executor() TypedExecutor { return p.executor }
func (p *graphqlPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionGraphQLQuery}
}

type wsPlugin struct{ executor *WebSocketExecutor }

func (p *wsPlugin) Name() string            { return "websocket" }
func (p *wsPlugin) Executor() TypedExecutor { return p.executor }
func (p *wsPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionWSConnect, types.ActionWSSend, types.ActionWSReceive, types.ActionWSDisconnect, types.ActionWSExpectClose}
}
