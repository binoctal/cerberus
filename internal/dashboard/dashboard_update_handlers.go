package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleWindowSize handles window size changes
func (m Model) handleWindowSize(msg tea.WindowSizeMsg) Model {
	m.width = msg.Width
	m.height = msg.Height
	return m
}

// handleQuit handles quit commands
func (m Model) handleQuit() (Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

// handleNavigation handles up/down navigation
func (m Model) handleNavigation(direction string) (Model, tea.Cmd) {
	switch direction {
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
	}
	return m, nil
}

// handleToggleDetail toggles detail view
func (m Model) handleToggleDetail() Model {
	m.detail = !m.detail
	return m
}

// handleRefresh triggers a refresh
func (m Model) handleRefresh() (Model, tea.Cmd) {
	return m, m.refresh()
}

// handleTickMsg handles timer tick messages
func (m Model) handleTickMsg() (Model, tea.Cmd) {
	return m, tea.Batch(m.refresh(), tick())
}

// handleRefreshMsg handles refresh completion messages
func (m Model) handleRefreshMsg(msg refreshMsg) Model {
	m.sessions = msg.sessions
	m.selected = msg.selected
	m.verdicts = msg.verdicts
	m.traces = msg.traces
	m.summary = msg.summary
	return m
}
