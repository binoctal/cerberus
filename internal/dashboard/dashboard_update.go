package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Update handles incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg), nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tickMsg:
		return m.handleTickMsg()

	case refreshMsg:
		return m.handleRefreshMsg(msg), nil

	default:
		return m, nil
	}
}
