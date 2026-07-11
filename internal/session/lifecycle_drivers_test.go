package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// TestSetupHeadDrivers_SharesSessionBudget verifies that per-head drivers
// share the session's single token budget. When each head gets its own
// budget, token consumption leaks out of session accounting and the session
// reports tokens_spent=0 even though many LLM calls were made.
func TestSetupHeadDrivers_SharesSessionBudget(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := project.DefaultConfig()
	cfg.Settings.AIBudget.SessionTotalTokens = 100000
	cfg.Settings.AIBudget.PerCallLimit = 10000

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:   ModeRun,
		Goal:   "budget sharing",
		Config: &cfg,
		Store:  s,
		Client: llm.NewMockClient(map[string]string{"default": `{}`}),
		Logger: zap.NewNop(),
	})
	require.NoError(t, err)

	// Tiers force per-head driver creation (mimics ANTHROPIC_DEFAULT_*_MODEL).
	tiers := config.TierModels{
		config.HeadScout: "glm-4.7",
		config.HeadAgent: "glm-4.5",
	}
	sess.SetClientFactory(func(llm.ClientConfig) (llm.Client, error) {
		return llm.NewMockClient(map[string]string{"default": `{}`}), nil
	})
	sess.SetupHeadDrivers("key", "https://example.test", llm.AuthSchemeAPIKey, tiers)

	require.NotNil(t, sess.scoutDriver, "scout driver should be configured from tier")

	sessionBudget := sess.Driver.Budget()
	before := sessionBudget.Remaining()

	// Token spent via a head driver must deplete the shared session budget.
	sess.scoutDriver.Budget().Record(1234)

	assert.Equal(t, before-1234, sessionBudget.Remaining(),
		"head driver must share the session budget; spending leaked out of session accounting")
}
