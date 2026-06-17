package dashboard

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

// refresh reloads sessions from the store.
func (m Model) refresh() tea.Cmd {
	ctx := context.Background()
	sessions, err := m.store.ListSessions(ctx, 50)
	if err != nil {
		return func() tea.Msg { return tickMsg{} }
	}
	if sessions == nil {
		sessions = []store.Session{}
	}

	prev := m.selected
	storeRef := m.store

	return func() tea.Msg {
		sel := normalizeSelection(prev, len(sessions))

		var verdicts []store.Verdict
		var traces []store.Trace
		var summary *session.SessionSummary

		if len(sessions) > 0 && sel < len(sessions) {
			verdicts, traces, summary = loadSessionData(*storeRef, sessions[sel])
		}

		return refreshMsg{
			sessions: sessions,
			selected: sel,
			verdicts: verdicts,
			traces:   traces,
			summary:  summary,
		}
	}
}

// loadSelected loads data for the currently selected session.
func (m Model) loadSelected() tea.Cmd {
	if len(m.sessions) == 0 {
		return nil
	}
	sel := m.selected
	sessions := m.sessions
	storeRef := m.store

	return func() tea.Msg {
		verdicts, traces, summary := loadSessionData(*storeRef, sessions[sel])

		return refreshMsg{
			sessions: sessions,
			selected: sel,
			verdicts: verdicts,
			traces:   traces,
			summary:  summary,
		}
	}
}
