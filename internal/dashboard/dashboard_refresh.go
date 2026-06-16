package dashboard

import (
	"context"
	"encoding/json"

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
		sel := prev
		if sel >= len(sessions) {
			sel = len(sessions) - 1
		}
		if sel < 0 {
			sel = 0
		}

		var verdicts []store.Verdict
		var traces []store.Trace
		var summary *session.SessionSummary

		if len(sessions) > 0 {
			id := sessions[sel].ID
			verdicts, _ = storeRef.GetVerdicts(ctx, id)
			traces, _ = storeRef.GetTraces(ctx, id)
			if sessions[sel].Stats != "" && sessions[sel].Stats != "{}" {
				var s session.SessionSummary
				if err := json.Unmarshal([]byte(sessions[sel].Stats), &s); err == nil {
					summary = &s
				}
			}
		}

		if verdicts == nil {
			verdicts = []store.Verdict{}
		}
		if traces == nil {
			traces = []store.Trace{}
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
		ctx := context.Background()
		id := sessions[sel].ID
		verdicts, _ := storeRef.GetVerdicts(ctx, id)
		traces, _ := storeRef.GetTraces(ctx, id)
		var summary *session.SessionSummary
		if sessions[sel].Stats != "" && sessions[sel].Stats != "{}" {
			var s session.SessionSummary
			if err := json.Unmarshal([]byte(sessions[sel].Stats), &s); err == nil {
				summary = &s
			}
		}
		if verdicts == nil {
			verdicts = []store.Verdict{}
		}
		if traces == nil {
			traces = []store.Trace{}
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
