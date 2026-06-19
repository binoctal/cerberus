package scout

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func localScout(t *testing.T) *Scout {
	t.Helper()
	cfg := project.DefaultConfig()
	cfg.Services = nil // no live target → local-only mode
	driver := ai.NewDriver(llm.NewMockClient(nil), ai.NewTokenBudget(100000, 10000))
	return NewScout(driver, setupTestStore(t), &cfg, zap.NewNop())
}

// TestBuildAIPrompt_LocalOnlyUsesLocalSystem verifies that Analyze switches to
// a local-codebase system prompt when there is no service URL. The SaaS
// prompt ("API surface") would otherwise make the LLM invent HTTP endpoints
// for a project that has no server.
func TestBuildAIPrompt_LocalOnlyUsesLocalSystem(t *testing.T) {
	s := localScout(t)
	prompt := s.buildAIPrompt(TargetInfo{Goal: "g", URL: ""})

	assert.Contains(t, prompt, "LOCAL CODEBASE")
	assert.NotContains(t, prompt, "SaaS project")
	assert.NotContains(t, prompt, "API surface")
}

func TestBuildAIPrompt_WithURLUsesSaaSSystem(t *testing.T) {
	s := localScout(t)
	prompt := s.buildAIPrompt(TargetInfo{Goal: "g", URL: "http://x.test"})

	assert.Contains(t, prompt, "SaaS project")
}

// TestBuildPlanningPrompt_LocalOnlyAvoidsHTTP verifies that Plan switches to a
// local system prompt (file/process/code executors) when there is no service
// URL, instead of the SaaS prompt that demands HTTP test cases per endpoint.
func TestBuildPlanningPrompt_LocalOnlyAvoidsHTTP(t *testing.T) {
	s := localScout(t)
	prompt := s.buildPlanningPrompt(context.Background(), "g", &project.ProjectModel{}, "")

	assert.Contains(t, prompt, "process_exec")
	assert.Contains(t, prompt, "file_read")
	assert.NotContains(t, prompt, "for EVERY endpoint")
}

func TestBuildPlanningPrompt_WithURLUsesSaaSSystem(t *testing.T) {
	cfg := project.DefaultConfig()
	cfg.Services = []project.Service{{Name: "web", URL: "http://x.test"}}
	driver := ai.NewDriver(llm.NewMockClient(nil), ai.NewTokenBudget(100000, 10000))
	s := NewScout(driver, setupTestStore(t), &cfg, zap.NewNop())

	prompt := s.buildPlanningPrompt(context.Background(), "g", &project.ProjectModel{}, "")
	assert.Contains(t, prompt, "for EVERY endpoint")
}

func TestIsLocalOnly(t *testing.T) {
	localCfg := project.DefaultConfig()
	localCfg.Services = nil
	sLocal := NewScout(nil, setupTestStore(t), &localCfg, zap.NewNop())
	assert.True(t, sLocal.isLocalOnly())

	svcCfg := project.DefaultConfig()
	svcCfg.Services = []project.Service{{Name: "web", URL: "http://x.test"}}
	sSvc := NewScout(nil, setupTestStore(t), &svcCfg, zap.NewNop())
	assert.False(t, sSvc.isLocalOnly())
}

// TestIsLocalOnly_ModeTakesPrecedence verifies explicit mode overrides the
// services-based inference, so users can force local vs SaaS without ambiguity.
func TestIsLocalOnly_ModeTakesPrecedence(t *testing.T) {
	localForced := project.DefaultConfig()
	localForced.Settings.Mode = project.ModeLocal
	localForced.Services = []project.Service{{Name: "web", URL: "http://x.test"}} // would infer SaaS
	s := NewScout(nil, setupTestStore(t), &localForced, zap.NewNop())
	assert.True(t, s.isLocalOnly(), "mode=local forces local-only even with services")

	saasForced := project.DefaultConfig()
	saasForced.Settings.Mode = project.ModeSaaS
	saasForced.Services = nil // would infer local
	s2 := NewScout(nil, setupTestStore(t), &saasForced, zap.NewNop())
	assert.False(t, s2.isLocalOnly(), "mode=saas forces SaaS even without services")
}
