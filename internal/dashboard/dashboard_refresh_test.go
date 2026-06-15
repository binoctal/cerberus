package dashboard

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func setupDashboardStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)
	return s
}

func TestModel_Refresh_WithData(t *testing.T) {
	s := setupDashboardStore(t)
	ctx := context.Background()

	dbSess, err := s.CreateSession(ctx, "run", "test goal", "test-project")
	require.NoError(t, err)
	require.NoError(t, s.UpdateSessionStatus(ctx, dbSess.ID, "completed"))

	summary := session.SessionSummary{Goal: "test goal", Passed: 5, Failed: 1}
	_ = summary
	require.NoError(t, s.UpdateSessionStats(ctx, dbSess.ID, 80.0, &summary))

	m := Model{store: s}
	cmd := m.refresh()
	require.NotNil(t, cmd)

	msg := cmd()
	refreshMsg, ok := msg.(refreshMsg)
	require.True(t, ok)
	assert.Len(t, refreshMsg.sessions, 1)
	assert.Equal(t, dbSess.ID, refreshMsg.sessions[0].ID)

	// Feed into Update.
	updated, _ := m.Update(refreshMsg)
	model := updated.(Model)
	assert.Len(t, model.sessions, 1)
}

func TestModel_Refresh_EmptyStore(t *testing.T) {
	s := setupDashboardStore(t)

	m := Model{store: s}
	cmd := m.refresh()
	require.NotNil(t, cmd)

	msg := cmd()
	refreshMsg, ok := msg.(refreshMsg)
	require.True(t, ok)
	assert.Empty(t, refreshMsg.sessions)
}

func TestModel_LoadSelected_WithData(t *testing.T) {
	s := setupDashboardStore(t)
	ctx := context.Background()

	dbSess, err := s.CreateSession(ctx, "run", "test goal", "proj")
	require.NoError(t, err)
	require.NoError(t, s.UpdateSessionStatus(ctx, dbSess.ID, "completed"))

	traceID, err := s.CreateTrace(ctx, dbSess.ID, "http", "GET /api")
	require.NoError(t, err)
	require.NoError(t, s.FinishTrace(ctx, traceID, "pass"))
	_, err = s.CreateVerdict(ctx, dbSess.ID, traceID, "GET /api", "pass", 0.95, "judge", "ok", nil, store.FailureReasonNone)
	require.NoError(t, err)

	sessions, err := s.ListSessions(ctx, 50)
	require.NoError(t, err)

	m := Model{store: s, sessions: sessions, selected: 0}
	cmd := m.loadSelected()
	require.NotNil(t, cmd)

	msg := cmd()
	refreshMsg, ok := msg.(refreshMsg)
	require.True(t, ok)
	assert.Len(t, refreshMsg.verdicts, 1)
	assert.Len(t, refreshMsg.traces, 1)
}

func TestModel_LoadSelected_EmptySessions(t *testing.T) {
	s := setupDashboardStore(t)
	m := Model{store: s, sessions: nil}

	cmd := m.loadSelected()
	assert.Nil(t, cmd, "should return nil cmd when no sessions")
}

func TestModel_Update_RefreshKey(t *testing.T) {
	s := setupDashboardStore(t)
	m := Model{store: s, width: 80, height: 24}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.NotNil(t, cmd, "pressing 'r' should trigger refresh cmd")
	_ = updated
}
