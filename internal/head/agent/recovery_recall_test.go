package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

type mockExecutorResult struct{}

func (mockExecutorResult) Success() bool                { return false }
func (mockExecutorResult) Duration() time.Duration      { return 0 }
func (mockExecutorResult) Summary() string              { return "action failed" }
func (mockExecutorResult) Evidence() types.EvidenceData { return types.EvidenceData{Type: "none"} }

func TestRecovery_RecallsByEmbeddingAndRecordsUsage(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../../migrations"))

	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	vec, _ := emb.Embed(ctx, "post /api/v1/* returned 401")
	_, err = s.StoreProceduralWithType(ctx, "test-memory-name", "post /api/v1/* returned 401", "retry auth", "test-project", "auth", "failure", vec, emb.ModelName())
	require.NoError(t, err)

	// S3: Recovery uses DecideWithTools. Preset an `api_request` tool call so
	// Recover assembles an action (the decision shape is irrelevant to this
	// test — only that recovery runs and exercises the L3 recall path).
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("default", []llm.ToolCall{{Name: "api_request", Input: map[string]any{
		"method": "GET", "url": "/x",
	}}})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(1000, 100))
	rec := agent.NewRecovery(driver, s, agent.DefaultReActConfig(), zap.NewNop(), emb)
	rec.SetSessionID("sess-9")
	rec.SetProject("test-project")

	_, err = rec.Recover(ctx, agent.TestCase{ID: "tc-1", Target: "/api/v1/login"}, mockExecutorResult{}, 1)
	require.NoError(t, err)

	var n int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_usage WHERE session_id='sess-9' AND case_id='tc-1'`).Scan(&n))
	require.Equal(t, 1, n, "recovery must record memory_usage for recalled L3")
}

// TestRecovery_HighThresholdSuppressesRecall verifies the recall threshold is
// taken from ReActConfig: a very high threshold suppresses recall (no
// memory_usage written), while the default threshold recalls (previous test).
func TestRecovery_HighThresholdSuppressesRecall(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../../migrations"))

	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	vec, _ := emb.Embed(ctx, "post /api/v1/* returned 401")
	_, err = s.StoreProceduralWithType(ctx, "n", "post /api/v1/* returned 401", "retry auth", "test-project", "auth", "failure", vec, emb.ModelName())
	require.NoError(t, err)

	cfg := agent.DefaultReActConfig()
	cfg.ProceduralRecallThreshold = 0.99 // only near-exact matches pass
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("default", []llm.ToolCall{{Name: "api_request", Input: map[string]any{
		"method": "GET", "url": "/x",
	}}})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(1000, 100))
	rec := agent.NewRecovery(driver, s, cfg, zap.NewNop(), emb)
	rec.SetSessionID("sess-hi")
	rec.SetProject("test-project")

	_, err = rec.Recover(ctx, agent.TestCase{ID: "tc-hi", Target: "/api/v1/login"}, mockExecutorResult{}, 1)
	require.NoError(t, err)

	var n int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_usage WHERE session_id='sess-hi'`).Scan(&n))
	require.Equal(t, 0, n, "high threshold must suppress recall → no memory_usage")
}
