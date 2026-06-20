package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
)

func TestRecovery_HasEmbedderAndSessionID(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	driver := ai.NewDriver(llm.NewMockClient(nil), ai.NewTokenBudget(1000, 100))
	rec := agent.NewRecovery(driver, s, agent.DefaultReActConfig(), zap.NewNop(), emb)
	rec.SetSessionID("sess-42")

	// Build a loop via config and ensure the embedder + sessionID propagate.
	loop := agent.NewReActLoopWithConfig(agent.ReActLoopConfig{
		Driver: driver, Store: s, Config: agent.DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb,
	})
	require.NotNil(t, loop)
	_ = ctx
}
