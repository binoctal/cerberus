//go:build live

package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// TestSteer_LiveGLM drives the migrated steer path through the real GLM LLM
// (loaded from .claude/settings.json via config.Load) to prove the migrated
// DecideWithTools + actionTools() surface produces a non-empty action tool
// call rather than drift. This is the action-emission analog of S2's
// TestScoutPlan_LiveGLM: it asserts GLM actually engages with the action tool
// definitions under the live provider, not just the mock fixture path.
//
// Build-tagged `live` so it never runs in `make test`. Lives in package
// agent_test (external) because the live test needs config.Load, which would
// form an import cycle (config → scout → agent) if the test lived inside
// package agent. SteerForLiveTest (in steer_export_test.go) exposes the
// unexported steer method for this single use. Run:
//
//	go test -tags live -run TestSteer_LiveGLM -v ./internal/head/agent/
func TestSteer_LiveGLM(t *testing.T) {
	cfg := config.Load()
	if cfg.LLMAPIKey == "" {
		t.Skip("no LLM API key in settings.json env — live probe needs a real LLM")
	}

	s, err := store.New(":memory:")
	require.NoError(t, err, "store")
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../../migrations"), "migrate")

	client, err := llm.NewClientWithConfig(llm.ClientConfig{
		Model:      cfg.LLMModel,
		APIKey:     cfg.LLMAPIKey,
		BaseURL:    cfg.LLMBaseURL,
		Provider:   cfg.LLMProvider,
		AuthScheme: cfg.LLMAuthScheme,
	})
	require.NoError(t, err, "llm client")
	driver := ai.NewDriver(client, ai.NewTokenBudget(60000, 10000))

	// Minimal ReAct loop: only steer is invoked, so the executor is never
	// called. A nil RuleEngine is fine because steer skips the base-URL hint
	// branch when tc.Service is empty (this probe leaves it empty). The
	// executor and embedder are still constructed so the loop's other fields
	// have valid non-nil defaults.
	executor := agent.BuildMultiExecutor(".", nil, nil, nil, zap.NewExample())
	loop := agent.NewReActLoopWithConfig(agent.ReActLoopConfig{
		Driver:   driver,
		Store:    s,
		Engine:   nil, // steer handles nil engine (tc.Service empty)
		Executor: executor,
		Config:   agent.DefaultReActConfig(),
		Logger:   zap.NewExample(),
		Embedder: embed.NewTrigramProvider(embed.DefaultDimension),
	})

	tc := &agent.TestCase{
		ID:          "live-steer-1",
		Name:        "hit health endpoint",
		Target:      "/health",
		Expectation: "returns 200 OK",
	}
	// prevResult=nil mirrors the first attempt in the ReAct loop (no prior
	// observation); formatResultContext tolerates a nil result.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	start := time.Now()
	action, zeroCall, err := agent.SteerForLiveTest(loop, ctx, tc, nil, 1)
	elapsed := time.Since(start)

	t.Logf("steer elapsed: %s", elapsed.Round(time.Millisecond))
	require.NoError(t, err, "steer must not surface a transient error on a fresh live call")
	assert.False(t, zeroCall,
		"zeroCall=true means GLM emitted no action tool call (drift) — the steer prompt/tool surface may need guidance adjustment")
	require.NotNil(t, action, "steer must return a non-nil TypedAction when zeroCall is false")

	actType := string(action.GetActionType())
	t.Logf("steer emitted action: type=%s", actType)
	assert.NotEmpty(t, actType, "assembled action must expose a non-empty ActionType")

	// Sanity: the emitted action type must be one of the LLM-reachable tools in
	// actionTools() (not a rule-engine-only type like ws_*/code_*/db_*). This
	// catches a future prompt regression where GLM starts naming tools outside
	// the steer surface.
	reachable := map[string]bool{
		"api_request": true, "navigate": true, "wait": true,
		"process_exec": true,
		"file_read":    true, "file_write": true, "file_exists": true, "file_glob": true,
		"browser_goto": true, "browser_click": true, "browser_fill": true, "browser_eval": true,
		"mcp_call": true,
	}
	assert.True(t, reachable[actType],
		"emitted action type %q must be one of the LLM-reachable action tools", actType)
}

// TestRecover_LiveGLM drives Recovery.Recover through the real GLM LLM with a
// preset failed result (ErrorResult describing a 500) and asserts GLM engages
// with the recovery tool surface — emitting either a real action tool call
// (retry) or the dedicated `skip` control tool (abandon). The recovery prompt
// changed substantially in S3 (lost the JSON Output block; added tool-use
// guidance + L3 memory mixing), so mock coverage alone cannot prove GLM parses
// the failure context correctly. This probe is the stretch counterpart to
// TestSteer_LiveGLM. Run:
//
//	go test -tags live -run TestRecover_LiveGLM -v ./internal/head/agent/
func TestRecover_LiveGLM(t *testing.T) {
	cfg := config.Load()
	if cfg.LLMAPIKey == "" {
		t.Skip("no LLM API key in settings.json env — live probe needs a real LLM")
	}

	s, err := store.New(":memory:")
	require.NoError(t, err, "store")
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../../migrations"), "migrate")

	client, err := llm.NewClientWithConfig(llm.ClientConfig{
		Model:      cfg.LLMModel,
		APIKey:     cfg.LLMAPIKey,
		BaseURL:    cfg.LLMBaseURL,
		Provider:   cfg.LLMProvider,
		AuthScheme: cfg.LLMAuthScheme,
	})
	require.NoError(t, err, "llm client")
	driver := ai.NewDriver(client, ai.NewTokenBudget(60000, 10000))

	// NewRecovery installs the default trigram embedder when given nil; L3
	// recall returns nothing because the empty in-memory store has no
	// procedural memories to match. The probe therefore exercises the
	// tool-emission path without L3 memory noise.
	rc := agent.NewRecovery(driver, s, agent.DefaultReActConfig(), zap.NewExample(), nil)

	tc := agent.TestCase{
		ID:          "live-recover-1",
		Name:        "retry 500",
		Target:      "/api/orders/42",
		Expectation: "returns 200 OK",
	}
	failed := types.ErrorResult{Err: "HTTP 500 Internal Server Error", Latency: 120 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	start := time.Now()
	dec, err := rc.Recover(ctx, tc, failed, 1)
	elapsed := time.Since(start)

	t.Logf("recover elapsed: %s", elapsed.Round(time.Millisecond))
	require.NoError(t, err, "Recover must surface a decision, not an error")

	// Mutually exclusive: either Skip==true (with nil Action) OR a real action
	// (with Skip==false). Either outcome proves GLM engaged with the recovery
	// tool surface — the failure mode we are gating on is silent silent drift
	// surfacing as an error rather than a decision.
	if dec.Skip {
		assert.Nil(t, dec.Action, "Skip=true must carry a nil Action (mutually exclusive)")
		t.Logf("recover decision: SKIP (target abandoned)")
	} else {
		require.NotNil(t, dec.Action, "Skip=false must carry a non-nil Action")
		actType := string(dec.Action.GetActionType())
		t.Logf("recover decision: RETRY with action type=%s", actType)
		assert.NotEmpty(t, actType, "recovered action must expose a non-empty ActionType")
	}
}
