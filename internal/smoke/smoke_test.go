package smoke

import (
	"context"
	"os"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSessionSmokeTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping smoke test in short mode")
	}

	dbURL := os.Getenv("CERBERUS_TEST_DB_URL")
	if dbURL == "" {
		dbURL = "postgres://cerberus:cerberus@localhost:5432/cerberus_test?sslmode=disable"
	}

	s, err := store.New(dbURL)
	require.NoError(t, err, "need a running PostgreSQL with cerberus_test DB")
	defer s.Close()

	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	mockResp := `{"status":"pass","confidence":0.9,"reasoning":"mock analysis"}`
	client := llm.NewMockClient(map[string]string{"default": mockResp})

	logger, _ := zap.NewDevelopment()

	cfg := project.DefaultConfig()
	cfg.Project.Name = "smoke-test"

	sess, err := session.NewSession(ctx, session.ModeRun, "smoke test goal", &cfg, s, client, logger)
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID)

	err = sess.Run(ctx)
	require.NoError(t, err)

	dbSess, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)
	assert.Equal(t, "smoke test goal", dbSess.Goal)

	sess.Close()
}

func TestAIDriverSmokeTest(t *testing.T) {
	mockResp := `{"verdict":"pass","confidence":0.95,"reasoning":"response matches expected"}`
	client := llm.NewMockClient(map[string]string{"default": mockResp})

	driver := ai.NewDriver(client, ai.NewTokenBudget(200000, 10000))

	type Verdict struct {
		Verdict    string  `json:"verdict"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	var v Verdict
	err := driver.Decide(context.Background(),
		ai.NewPrompt().
			System("You are a test judge").
			Task("Evaluate: POST /api/v1/users returned 201").
			Output("JSON with verdict, confidence, reasoning").
			Build(),
		&v,
	)
	require.NoError(t, err)
	assert.Equal(t, "pass", v.Verdict)
	assert.InDelta(t, 0.95, v.Confidence, 0.01)
	assert.Less(t, driver.Budget().Remaining(), 200000)
}

func TestProjectLoaderSmokeTest(t *testing.T) {
	os.Setenv("TEST_URL", "http://localhost:8080")
	defer os.Unsetenv("TEST_URL")

	yaml := `
project:
  name: smoke-app
services:
  - name: api
    url: "${TEST_URL}"
settings:
  confidence_threshold: 0.8
`
	cfg, err := project.LoadFromYAML([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "smoke-app", cfg.Project.Name)
	assert.Equal(t, "http://localhost:8080", cfg.Services[0].URL)
	assert.Equal(t, 0.8, cfg.Settings.ConfidenceThreshold)

	assert.Equal(t, 200000, cfg.Settings.AIBudget.SessionTotalTokens)
}
