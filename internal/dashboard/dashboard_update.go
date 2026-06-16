package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Update handles incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.detail = false
				return m, m.loadSelected()
			}
		case "down", "j":
			if m.selected < len(m.sessions)-1 {
				m.selected++
				m.detail = false
				return m, m.loadSelected()
			}
		case "enter":
			m.detail = !m.detail
			return m, nil
		case "r":
			return m, m.refresh()
		}

	case tickMsg:
		return m, tea.Batch(m.refresh(), tick())

	case refreshMsg:
		m.sessions = msg.sessions
		m.selected = msg.selected
		m.verdicts = msg.verdicts
		m.traces = msg.traces
		m.summary = msg.summary
		return m, nil
	}

	return m, nil
}
