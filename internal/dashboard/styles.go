package dashboard

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#1e40af")).
			Padding(0, 2)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#64748b")).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#334155")).
			Foreground(lipgloss.Color("#ffffff"))

	passStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#22c55e"))

	failStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ef4444"))

	uncertainStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#eab308"))

	skipStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9ca3af"))

	runningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3b82f6"))

	detailBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#475569")).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94a3b8")).
			Padding(0, 1)
)

// statusColor returns the styled status string.
func statusColor(status string) string {
	switch status {
	case "pass", "passed", "completed":
		return passStyle.Render(status)
	case "fail", "failed":
		return failStyle.Render(status)
	case "uncertain":
		return uncertainStyle.Render(status)
	case "skip", "skipped":
		return skipStyle.Render(status)
	case "running":
		return runningStyle.Render(status)
	default:
		return status
	}
}
