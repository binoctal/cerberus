package dashboard

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/store"
)

// --- truncate() tests ---

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"truncated with ellipsis", "hello world", 8, "hello..."},
		{"empty string", "", 5, ""},
		{"maxLen 3", "abcdef", 3, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, truncate(tt.input, tt.maxLen))
		})
	}
}

// --- minInt() tests ---

func TestMinInt(t *testing.T) {
	assert.Equal(t, 3, minInt(3, 5))
	assert.Equal(t, 2, minInt(10, 2))
	assert.Equal(t, 0, minInt(0, 0))
	assert.Equal(t, -1, minInt(-1, 1))
}

// --- statusColor() tests ---

func TestStatusColor(t *testing.T) {
	tests := []struct {
		status string
		// We can't easily compare styled output, so just verify it's non-empty
		// and differs from input for known statuses.
	}{
		{"pass"}, {"passed"}, {"completed"},
		{"fail"}, {"failed"},
		{"uncertain"},
		{"skip"}, {"skipped"},
		{"running"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := statusColor(tt.status)
			assert.NotEmpty(t, result)
		})
	}
}

func TestStatusColor_Unknown(t *testing.T) {
	// Unknown status returns the raw string.
	assert.Equal(t, "unknown_status", statusColor("unknown_status"))
}

// --- Model.Update() tests ---

func TestModel_Update_Quit(t *testing.T) {
	m := Model{}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.True(t, updated.(Model).quitting)
	assert.NotNil(t, cmd) // tea.Quit is a func, can't compare directly
}

func TestModel_Update_CtrlC(t *testing.T) {
	m := Model{}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.True(t, updated.(Model).quitting)
	assert.NotNil(t, cmd)
}

func TestModel_Update_NavigateUp(t *testing.T) {
	m := Model{selected: 2, sessions: make([]store.Session, 5)}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 1, updated.(Model).selected)
}

func TestModel_Update_NavigateUpAtTop(t *testing.T) {
	m := Model{selected: 0, sessions: make([]store.Session, 5)}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, updated.(Model).selected)
}

func TestModel_Update_NavigateDown(t *testing.T) {
	m := Model{selected: 1, sessions: make([]store.Session, 5)}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, updated.(Model).selected)
}

func TestModel_Update_NavigateDownAtBottom(t *testing.T) {
	m := Model{selected: 4, sessions: make([]store.Session, 5)}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 4, updated.(Model).selected)
}

func TestModel_Update_NavigateK(t *testing.T) {
	m := Model{selected: 3, sessions: make([]store.Session, 5)}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	assert.Equal(t, 2, updated.(Model).selected)
}

func TestModel_Update_NavigateJ(t *testing.T) {
	m := Model{selected: 1, sessions: make([]store.Session, 5)}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	assert.Equal(t, 2, updated.(Model).selected)
}

func TestModel_Update_EnterToggle(t *testing.T) {
	m := Model{detail: false}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.True(t, updated.(Model).detail)

	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, updated.(Model).detail)
}

func TestModel_Update_WindowSize(t *testing.T) {
	m := Model{}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	um := updated.(Model)
	assert.Equal(t, 120, um.width)
	assert.Equal(t, 40, um.height)
}

// --- Model.View() tests ---

func TestView_Quitting(t *testing.T) {
	m := Model{quitting: true}
	assert.Equal(t, "", m.View())
}

func TestView_NoSessions(t *testing.T) {
	m := Model{sessions: nil}
	view := m.View()
	require.NotEmpty(t, view)
	assert.Contains(t, view, "No sessions found")
}

func TestView_WithSessions(t *testing.T) {
	m := Model{
		sessions: []store.Session{
			{ID: "abcdef1234567890", Status: "completed", Goal: "test all APIs"},
		},
		selected: 0,
	}
	view := m.View()
	require.NotEmpty(t, view)
	assert.Contains(t, view, "abcdef1") // truncated ID
	assert.Contains(t, view, "test all APIs")
}

func TestView_MultipleSessions(t *testing.T) {
	m := Model{
		sessions: []store.Session{
			{ID: "aaaaaaaa00000001", Status: "completed", Goal: "g1"},
			{ID: "bbbbbbbb00000002", Status: "running", Goal: "g2"},
		},
		selected: 0,
	}
	view := m.View()
	assert.Contains(t, view, "1 completed, 1 running")
}

func TestView_HelpLine(t *testing.T) {
	m := Model{}
	view := m.View()
	assert.Contains(t, view, "q: quit")
}

// --- refreshMsg handling ---

func TestModel_Update_RefreshMsg(t *testing.T) {
	sessions := []store.Session{
		{ID: "test12345678", Status: "pass"},
	}
	m := Model{}
	updated, _ := m.Update(refreshMsg{
		sessions: sessions,
		selected: 0,
	})
	um := updated.(Model)
	assert.Equal(t, sessions, um.sessions)
	assert.Equal(t, 0, um.selected)
}

// --- View contains dashboard title ---

func TestView_Title(t *testing.T) {
	m := Model{}
	view := m.View()
	assert.True(t, strings.Contains(view, "Cerberus Dashboard"), "view should contain dashboard title")
}
