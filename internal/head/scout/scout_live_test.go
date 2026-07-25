//go:build live

package scout_test

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// TestScoutRelayEmission_Live drives Scout.Plan with the real LLM (loaded from
// .claude/settings.json via config.Load) against a two-role WS protocol and a
// relay goal, then dumps the generated cases to inspect whether the LLM emits a
// ws_relay intent (the A1 risk for Scout relay generation). Build-tagged `live`
// so it never runs in `make test`. Run:
//
//	go test -tags live -run TestScoutRelayEmission_Live -v ./internal/head/scout/
func TestScoutRelayEmission_Live(t *testing.T) {
	cfg := config.Load()
	if cfg.LLMAPIKey == "" {
		t.Skip("no LLM API key in settings.json env — live probe needs a real LLM")
	}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := store.RunMigrations(context.Background(), s.DB(), "../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	projCfg := &project.Config{
		Services: []project.Service{{
			Name: "realtime",
			URL:  "http://localhost:8989/ws/demo_user",
			Protocol: &project.Protocol{
				TypePath: "type",
				Auth:     &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web"},
				Roles: map[string]*project.ProtocolRole{
					"web":    {CredentialRef: "web", Params: map[string]string{"type": "web"}, Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
					"bridge": {CredentialRef: "bridge", Params: map[string]string{"type": "bridge"}},
				},
			},
		}},
		Actors: []project.Actor{{Name: "web"}, {Name: "bridge"}},
	}

	client, err := llm.NewClientWithConfig(llm.ClientConfig{
		Model:      cfg.LLMModel,
		APIKey:     cfg.LLMAPIKey,
		BaseURL:    cfg.LLMBaseURL,
		Provider:   cfg.LLMProvider,
		AuthScheme: cfg.LLMAuthScheme,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	driver := ai.NewDriver(client, ai.NewTokenBudget(60000, 10000))
	sct := scout.NewScout(driver, s, projCfg, zap.NewExample())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	goal := "The web client connects and the bridge client connects to the realtime service. The web client sends a session:start message; the server relays it to the bridge client, which receives the relayed session:start. Verify the bridge receives session:start relayed from the web client while both are connected."
	plan, err := sct.Plan(ctx, goal, &project.ProjectModel{})
	if err != nil {
		t.Fatalf("scout plan: %v", err)
	}

	t.Logf("=== %d cases generated ===", len(plan.Cases))
	sawRelay, sawMultiStep := false, false
	for i, c := range plan.Cases {
		t.Logf("[%d] id=%q action=%q service=%q target=%q", i, c.ID, c.Action, c.Service, c.Target)
		if c.Body != "" {
			t.Logf("    body=%s", truncLive(c.Body, 220))
		}
		if len(c.Steps) > 1 {
			sawMultiStep = true
		}
		for j, st := range c.Steps {
			t.Logf("    step[%d] %s conn=%s role=%s type=%s", j, st.Action, st.ConnectionID, st.Role, st.Type)
		}
		if c.Action == "ws_relay" {
			sawRelay = true
		}
	}
	t.Logf("=== saw ws_relay case: %v | saw multi-step (>1) case: %v ===", sawRelay, sawMultiStep)
}

func truncLive(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// TestScoutPlan_LiveGLM drives Scout.Plan through the real DecideWithTools
// path against a live LLM (loaded from .claude/settings.json via config.Load).
// The goal is a simple HTTP-only CRUD surface with no WS protocol — this
// validates that the migrated directPlan path emits test_http_endpoint tool
// calls and that assemblePlan produces at least one case. Build-tagged `live`
// so it never runs in `make test`. Run:
//
//	go test -tags live -run TestScoutPlan_LiveGLM -v ./internal/head/scout/
func TestScoutPlan_LiveGLM(t *testing.T) {
	cfg := config.Load()
	if cfg.LLMAPIKey == "" {
		t.Skip("no LLM API key in settings.json env — live probe needs a real LLM")
	}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := store.RunMigrations(context.Background(), s.DB(), "../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	projCfg := &project.Config{
		Project: project.ProjectMeta{Name: "live-direct-plan"},
		Services: []project.Service{{
			Name:   "api",
			URL:    "http://localhost:8080",
			Health: "/health",
		}},
	}

	client, err := llm.NewClientWithConfig(llm.ClientConfig{
		Model:      cfg.LLMModel,
		APIKey:     cfg.LLMAPIKey,
		BaseURL:    cfg.LLMBaseURL,
		Provider:   cfg.LLMProvider,
		AuthScheme: cfg.LLMAuthScheme,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	driver := ai.NewDriver(client, ai.NewTokenBudget(60000, 10000))
	sct := scout.NewScout(driver, s, projCfg, zap.NewExample())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	goal := "Verify the API health endpoint responds with 200 OK and the /api/v1/users endpoint returns a non-empty list."
	plan, err := sct.Plan(ctx, goal, &project.ProjectModel{
		API: project.APIModel{Endpoints: []project.EndpointDef{
			{Method: "GET", Path: "/health", Confidence: 0.95},
			{Method: "GET", Path: "/api/v1/users", Confidence: 0.9},
		}},
	})
	if err != nil {
		t.Fatalf("scout plan: %v", err)
	}

	t.Logf("=== %d cases generated ===", len(plan.Cases))
	if len(plan.Cases) == 0 {
		t.Fatalf("expected at least one case from live LLM")
	}
	httpCases := 0
	for i, c := range plan.Cases {
		t.Logf("[%d] id=%q name=%q method=%q target=%q action=%q expectation=%q",
			i, c.ID, c.Name, c.Method, c.Target, c.Action, c.Expectation)
		if c.Method != "" {
			httpCases++
		}
	}
	t.Logf("=== http-method cases: %d / %d ===", httpCases, len(plan.Cases))
	if httpCases == 0 {
		t.Fatalf("expected at least one HTTP case from live LLM (got %d total cases)", len(plan.Cases))
	}
}
