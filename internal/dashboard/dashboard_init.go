package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Init initializes the dashboard model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tick(), tea.EnterAltScreen)
}
