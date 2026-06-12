package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateExecutorCases_Go(t *testing.T) {
	info := ProjectInfo{Type: ProjectGo, RootDir: "/project", Language: "Go"}
	cases := GenerateExecutorCases(info, "test the API")

	assert.Len(t, cases, 4)
	assert.Equal(t, "exec-001", cases[0].ID)
	assert.Equal(t, "process_build", cases[0].Action)
	assert.Equal(t, "exec-002", cases[1].ID)
	assert.Equal(t, "process_exec", cases[1].Action)
	assert.Equal(t, "exec-003", cases[2].ID)
	assert.Equal(t, "process_exec", cases[2].Action)
	assert.Equal(t, "exec-004", cases[3].ID)
	assert.Equal(t, "code_symbols", cases[3].Action)
}

func TestGenerateExecutorCases_Node(t *testing.T) {
	info := ProjectInfo{Type: ProjectNode, RootDir: "/project", BuildCmd: "npm install", TestCmd: "npm test", Language: "JavaScript/TypeScript"}
	cases := GenerateExecutorCases(info, "test the app")

	assert.Len(t, cases, 4)
	assert.Equal(t, "exec-001", cases[0].ID)
	assert.Equal(t, "process_exec", cases[0].Action)
	assert.Equal(t, "npm install", cases[0].Target)
	assert.Equal(t, "exec-002", cases[1].ID)
	assert.Equal(t, "process_exec", cases[1].Action)
	assert.Equal(t, "exec-003", cases[2].ID)
	assert.Equal(t, "code_lint", cases[2].Action)
	assert.Equal(t, "JavaScript/TypeScript", cases[2].Language)
	assert.Equal(t, "exec-004", cases[3].ID)
	assert.Equal(t, "code_symbols", cases[3].Action)
}

func TestGenerateExecutorCases_Python(t *testing.T) {
	info := ProjectInfo{Type: ProjectPython, RootDir: "/project", Language: "Python"}
	cases := GenerateExecutorCases(info, "test the code")

	assert.Len(t, cases, 3)
	assert.Equal(t, "exec-001", cases[0].ID)
	assert.Equal(t, "process_exec", cases[0].Action)
	assert.Equal(t, "exec-002", cases[1].ID)
	assert.Equal(t, "code_lint", cases[1].Action)
	assert.Equal(t, "Python", cases[1].Language)
	assert.Equal(t, "exec-003", cases[2].ID)
	assert.Equal(t, "code_symbols", cases[2].Action)
}

func TestGenerateExecutorCases_HTTP(t *testing.T) {
	info := ProjectInfo{Type: ProjectHTTP}
	cases := GenerateExecutorCases(info, "test the API")
	assert.Nil(t, cases)
}

func TestGenerateExecutorCases_Unknown(t *testing.T) {
	info := ProjectInfo{Type: ProjectUnknown}
	cases := GenerateExecutorCases(info, "test")
	assert.Nil(t, cases)
}
