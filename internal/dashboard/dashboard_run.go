package dashboard

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/binoctal/cerberus/internal/store"
)

// Run starts the dashboard TUI.
func Run(s *store.Store) error {
	p := tea.NewProgram(
		Model{store: s},
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}

// tick creates a periodic tick command.
func tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}
