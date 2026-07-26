//go:build live

package scout_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// TestScoutRelayEmission_Live drives Scout.Plan with the real LLM (loaded from
// .claude/settings.json via config.Load) against a two-role WS protocol and a
// relay goal, then dumps the generated cases to inspect whether the LLM emits a
// ws_flow choreography via begin_case+ws_* tools (the A1 risk for Scout relay
// generation). Pre-S2 this was a "ws_relay" structured-output intent, which GLM
// never emitted (see cerberus-docs/technical/dogfood/2026-07-24-ws-scout-relay-llm-dogfood-procedure.md);
// S2 renamed it to a ws_flow case assembled from begin_case+ws_* tool calls.
// Cases are categorized (http/ws_flow/invariant/process/…) so action=""
// (check_invariant → invariant assertion, deliberately Action-less) is not
// misread as a malformed case. Build-tagged `live` so it never runs in `make
// test`. Run:
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
	sawFlow, sawMultiStep := false, false
	counts := map[string]int{}
	for i, c := range plan.Cases {
		// Categorize so action="" (check_invariant → invariant assertion,
		// deliberately Action-less) is not misread as a malformed case.
		cat := c.Action
		if c.Method != "" {
			cat = "http"
		} else if c.Action == "" {
			cat = "invariant"
		}
		counts[cat]++
		t.Logf("[%d] id=%q cat=%s action=%q service=%q target=%q", i, c.ID, cat, c.Action, c.Service, c.Target)
		if c.Body != "" {
			t.Logf("    body=%s", truncLive(c.Body, 220))
		}
		if len(c.Steps) > 1 {
			sawMultiStep = true
		}
		for j, st := range c.Steps {
			t.Logf("    step[%d] %s conn=%s role=%s type=%s", j, st.Action, st.ConnectionID, st.Role, st.Type)
		}
		if c.Action == "ws_flow" {
			sawFlow = true
		}
	}
	t.Logf("=== categories: %+v ===", counts)
	t.Logf("=== saw ws_flow case: %v | saw multi-step (>1) case: %v ===", sawFlow, sawMultiStep)
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

// TestBuildCoverageContract_LiveGLM drives Scout.BuildCoverageContract through
// the real DecideWithTools path against a live LLM (loaded from
// .claude/settings.json via config.Load). Validates that GLM emits the six
// contract tools (declare_scope/path_types/error_scope/boundaries, set_priority,
// set_coverage_gate) and that assembleContract produces a populated contract.
// Build-tagged `live` so it never runs in `make test`. Run:
//
//	go test -tags live -run TestBuildCoverageContract_LiveGLM -v ./internal/head/scout/
func TestBuildCoverageContract_LiveGLM(t *testing.T) {
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
		Project: project.ProjectMeta{Name: "live-contract"},
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
	c, err := sct.BuildCoverageContract(ctx, goal, &project.ProjectModel{
		API: project.APIModel{Endpoints: []project.EndpointDef{
			{Method: "GET", Path: "/health", Confidence: 0.95},
			{Method: "GET", Path: "/api/v1/users", Confidence: 0.9},
		}},
	}, contract.DepthStandard)
	if err != nil {
		t.Fatalf("build coverage contract: %v", err)
	}

	t.Logf("=== contract assembled ===")
	t.Logf("depth: %s", c.Depth)
	t.Logf("scope: %v", c.Scope)
	t.Logf("path_types: %v", c.PathTypes)
	t.Logf("error_scope: %v", c.ErrorScope)
	t.Logf("boundaries: %v", c.Boundaries)
	t.Logf("priorities: %+v", c.Priorities)
	t.Logf("coverage_gate: module=%s line=%.2f branch=%.2f",
		c.CoverageGate.Module, c.CoverageGate.LineThreshold, c.CoverageGate.BranchThreshold)

	assert.Equal(t, contract.DepthStandard, c.Depth, "depth must echo the depth parameter")
	require.NotEmpty(t, c.Scope, "expected declare_scope to populate Scope")
	require.NotEmpty(t, c.PathTypes, "expected declare_path_types to populate PathTypes")
	require.NotEmpty(t, c.ErrorScope, "expected declare_error_scope to populate ErrorScope")
	require.NotEmpty(t, c.Boundaries, "expected declare_boundaries to populate Boundaries")
	require.NotEmpty(t, c.CoverageGate.Module, "expected set_coverage_gate to populate Module")

	// The live probe checks emission of the five structural contract tools
	// (declare_scope/path_types/error_scope/boundaries, set_coverage_gate).
	// set_priority is conditional: GLM does not reliably emit it, so a hard
	// assertion would flake. When Priorities is non-empty, the bucket values
	// must be []string — but that []string enforcement is governed by the
	// set_priority schema and covered by the unit test
	// TestAssembleContract_PrioritiesForcedStringSlice (which deletes the
	// Priorities.UnmarshalJSON dual-shape patch).
	if len(c.Priorities) > 0 {
		for bucket, mods := range c.Priorities {
			t.Logf("priority[%s] = %v (type %T)", bucket, mods, mods)
			assert.NotNil(t, mods, "priority bucket %q must be []string, not bare string", bucket)
		}
	}
}
