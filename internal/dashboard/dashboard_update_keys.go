package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleKeyMsg handles keyboard input messages
func (m Model) handleKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m.handleQuit()
	case "up", "k":
		return m.handleNavigation("up")
	case "down", "j":
		return m.handleNavigation("down")
	case "enter":
		return m.handleToggleDetail(), nil
	case "r":
		return m.handleRefresh()
	default:
		return m, nil
	}
}
