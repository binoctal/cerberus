package smoke

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestDogfood_LocalProjectMode(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	// Mock LLM returns valid but minimal analysis/plan results.
	mockResp := `{"status":"pass","confidence":0.9,"reasoning":"mock analysis"}`
	client := llm.NewMockClient(map[string]string{"default": mockResp})
	// S2 tool-calling: Scout.Plan consumes DecideWithTools. Preset one
	// process_exec case keyed on the goal substring.
	client.SetToolResponse("dogfood test", []llm.ToolCall{
		{Name: "run_process", Input: map[string]any{"action": "exec", "cmd": "go build ./..."}},
	})
	logger := zap.NewNop()

	cfg := project.DefaultConfig()
	cfg.Project.Name = "cerberus-dogfood"

	sess, err := session.NewSession(ctx, session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "dogfood test",
		Config:     &cfg,
		Store:      s,
		Client:     client,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: "../..",
		CoverageFn: smokeCoverageFn(),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID)

	err = sess.Run(ctx)
	require.NoError(t, err)

	// Verify session completed.
	dbSess, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)

	sess.Close()
}

func TestDogfood_DetectProjectType(t *testing.T) {
	// Cerberus is a Go project; detect from project root.
	info := scout.DetectProjectType("../..")
	assert.Equal(t, scout.ProjectGo, info.Type)
	assert.Equal(t, "go build ./...", info.BuildCmd)
	assert.Equal(t, "go test ./...", info.TestCmd)
	assert.Equal(t, "go vet ./...", info.LintCmd)
}

func TestDogfood_GenerateExecutorCases(t *testing.T) {
	info := scout.DetectProjectType("../..")
	cases := scout.GenerateExecutorCases(info, "test this project")
	require.Len(t, cases, 4)

	// Verify the expected actions are present.
	buildFound := false
	testFound := false
	for _, tc := range cases {
		if tc.Action == "process_build" {
			buildFound = true
			assert.Equal(t, "go build ./...", tc.Target)
		}
		if tc.Action == "process_exec" && tc.Target == "go test ./..." {
			testFound = true
		}
	}
	assert.True(t, buildFound, "should have a process_build case")
	assert.True(t, testFound, "should have a process_exec test case")
}

func TestDogfood_RuleEngineExecCases(t *testing.T) {
	services := []project.Service{{Name: "default", URL: ""}}
	engine := agent.NewRuleEngine(services, nil, "../..")

	info := scout.DetectProjectType("../..")
	cases := scout.GenerateExecutorCases(info, "test this project")

	matched := 0
	for _, tc := range cases {
		action, ok := engine.Match(tc)
		if ok {
			matched++
			assert.NotNil(t, action)
		}
	}
	// All executor cases should be matched by the rule engine.
	assert.Equal(t, len(cases), matched, "all executor cases should be rule-matched without Steer")
}
