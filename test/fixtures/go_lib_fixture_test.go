package fixtures

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestGoLibFixture(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../migrations"))

	cfg := project.DefaultConfig()
	cfg.Settings.Mode = "local"
	mockClient := MockClient("math.go")

	sess, err := session.NewSession(context.Background(), session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "test go-lib fixture",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     zap.NewNop(),
		ProjectDir: "test/fixtures/go-lib",
	})
	require.NoError(t, err)

	err = sess.Run(context.Background())
	require.NoError(t, err, "session should complete")

	assert.NotNil(t, sess.Contract, "coverage contract should be produced")
	assert.NotEmpty(t, sess.ID, "session ID assigned")
}
