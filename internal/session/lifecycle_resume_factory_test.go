package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// NewSessionForResume must bind to the supplied ID WITHOUT inserting a new
// sessions row — otherwise --resume leaves the freshly-inserted row orphaned
// (state=running, never finalized) while writing verdicts into the old ID.
func TestNewSessionForResumeDoesNotInsertSession(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../migrations"))

	cfg := project.DefaultConfig()
	cfg.Project.Name = "test-project"
	mockClient := llm.NewMockClient(map[string]string{"default": `{"status":"pass"}`})

	count := func() int {
		var n int
		require.NoError(t, s.DB().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n))
		return n
	}
	before := count()

	sess, err := NewSessionForResume(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "g",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     zap.NewNop(),
		ProjectDir: ".",
	}, "resume-id-123")
	require.NoError(t, err)
	assert.Equal(t, "resume-id-123", sess.ID)
	assert.Equal(t, before, count(),
		"resume must not insert a new session row (would orphan it)")
}
