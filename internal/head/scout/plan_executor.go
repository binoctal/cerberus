package scout

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// GenerateExecutorCases produces non-HTTP test cases based on the detected
// project type. Returns nil for HTTP and unknown projects.
func GenerateExecutorCases(info ProjectInfo, goal string) []agent.TestCase {
	switch info.Type {
	case ProjectGo:
		return goExecutorCases(info, goal)
	case ProjectNode:
		return nodeExecutorCases(info, goal)
	case ProjectPython:
		return pythonExecutorCases(info, goal)
	default:
		return nil
	}
}

func goExecutorCases(info ProjectInfo, goal string) []agent.TestCase {
	return []agent.TestCase{
		{
			ID:          "exec-001",
			Name:        "Go project compiles",
			Target:      info.BuildCmd,
			Action:      "process_build",
			Expectation: "Build completes without errors",
			Priority:    0.9,
		},
		{
			ID:          "exec-002",
			Name:        "Go tests pass",
			Target:      info.TestCmd,
			Action:      "process_exec",
			Expectation: "All tests pass",
			Priority:    0.85,
		},
		{
			ID:          "exec-003",
			Name:        "Go vet finds no issues",
			Target:      info.LintCmd,
			Action:      "process_exec",
			Expectation: "No vet warnings",
			Priority:    0.6,
		},
		{
			ID:          "exec-004",
			Name:        "Symbol inventory collected",
			Target:      info.RootDir,
			Action:      "code_symbols",
			Expectation: fmt.Sprintf("Symbol table generated for %s project", info.Language),
			Priority:    0.5,
		},
	}
}

func nodeExecutorCases(info ProjectInfo, goal string) []agent.TestCase {
	return []agent.TestCase{
		{
			ID:          "exec-001",
			Name:        "Node dependencies installed",
			Target:      info.BuildCmd,
			Action:      "process_exec",
			Expectation: "npm install completes without errors",
			Priority:    0.9,
		},
		{
			ID:          "exec-002",
			Name:        "Node tests pass",
			Target:      info.TestCmd,
			Action:      "process_exec",
			Expectation: "All tests pass",
			Priority:    0.85,
		},
	}
}

func pythonExecutorCases(info ProjectInfo, goal string) []agent.TestCase {
	return []agent.TestCase{
		{
			ID:          "exec-001",
			Name:        "Python tests pass",
			Target:      info.TestCmd,
			Action:      "process_exec",
			Expectation: "All tests pass",
			Priority:    0.85,
		},
		{
			ID:          "exec-002",
			Name:        "Python lint clean",
			Target:      info.LintCmd,
			Action:      "code_lint",
			Expectation: "No lint errors",
			Priority:    0.7,
		},
	}
}
