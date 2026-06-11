package mcp

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestServer_CrashRecovery(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	// Create a "running" session (simulating crash).
	sess, err := s.CreateSession(ctx, "run", "test goal", "project")
	require.NoError(t, err)

	srv := NewServer(s, zap.NewNop())
	srv.RecoverOrphanSessions(ctx)

	// The orphan should be marked as interrupted.
	recovered, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "interrupted", recovered.Status)
}
