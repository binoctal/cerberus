package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

// tickMsg triggers a periodic data refresh.
type tickMsg time.Time

// refreshMsg carries refreshed data from the store.
type refreshMsg struct {
	sessions []store.Session
	selected int
	verdicts []store.Verdict
	traces   []store.Trace
	summary  *session.SessionSummary
}

// Model is the bubbletea model for the dashboard.
type Model struct {
	store    *store.Store
	sessions []store.Session
	selected int
	verdicts []store.Verdict
	traces   []store.Trace
	summary  *session.SessionSummary
	width    int
	height   int
	detail   bool // whether detail view is shown
	quitting bool
}

// Run starts the dashboard TUI.
func Run(s *store.Store) error {
	p := tea.NewProgram(
		Model{store: s},
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tick(), tea.EnterAltScreen)
}

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

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("🛡️ Cerberus Dashboard"))
	b.WriteString("\n")

	// Session counts.
	completed, running := 0, 0
	for _, s := range m.sessions {
		switch s.Status {
		case "completed":
			completed++
		case "running":
			running++
		}
	}
	b.WriteString(statusStyle.Render(fmt.Sprintf("Sessions: %d completed, %d running", completed, running)))
	b.WriteString("\n\n")

	if len(m.sessions) == 0 {
		b.WriteString("No sessions found. Run `cerberus run --goal \"...\"` to start.\n")
	} else {
		sess := m.sessions[m.selected]

		// Session header.
		fmt.Fprintf(&b, "Session: %s  |  Status: %s  |  Goal: %s\n",
			sess.ID[:8], statusColor(sess.Status), sess.Goal)
		if sess.ProjectName != "" {
			fmt.Fprintf(&b, "Project: %s\n", sess.ProjectName)
		}
		b.WriteString("\n")

		// Summary.
		if m.summary != nil && m.summary.TotalCases > 0 {
			s := m.summary
			fmt.Fprintf(&b, "Summary: %s %s %s %s %s\n",
				passStyle.Render(fmt.Sprintf("%d pass", s.Passed)),
				failStyle.Render(fmt.Sprintf("%d fail", s.Failed)),
				skipStyle.Render(fmt.Sprintf("%d skip", s.Skipped)),
				uncertainStyle.Render(fmt.Sprintf("%d uncertain", s.Uncertain)),
				fmt.Sprintf("(%d total)", s.TotalCases),
			)
			if s.Duration != "" {
				fmt.Fprintf(&b, "Duration: %s  |  Tokens: ~%dK\n", s.Duration, s.TotalTokens/1000)
			}
			b.WriteString("\n")
		}

		// Verdicts.
		if len(m.verdicts) > 0 {
			if m.detail {
				b.WriteString(detailBox.Render("Verdict Details"))
				b.WriteString("\n")
				for _, v := range m.verdicts {
					fmt.Fprintf(&b, "  %s %s  conf:%.2f\n",
						statusColor(v.Status), v.Target, v.Confidence)
					if v.Reasoning != "" {
						fmt.Fprintf(&b, "    %s\n", v.Reasoning)
					}
				}
			} else {
				b.WriteString("Verdicts:\n")
				maxShow := minInt(len(m.verdicts), 10)
				for _, v := range m.verdicts[:maxShow] {
					fmt.Fprintf(&b, "  %s %-40s  conf:%.2f\n",
						statusColor(v.Status), truncate(v.Target, 40), v.Confidence)
				}
				if len(m.verdicts) > 10 {
					fmt.Fprintf(&b, "  ... and %d more (press Enter for details)\n", len(m.verdicts)-10)
				}
			}
			b.WriteString("\n")
		}

		// Session list (compact).
		if len(m.sessions) > 1 {
			b.WriteString("─ ")
			for i, s := range m.sessions {
				label := fmt.Sprintf("[%s]", s.ID[:8])
				if i == m.selected {
					label = selectedStyle.Render(label)
				}
				b.WriteString(label + " ")
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑↓/jk: navigate  Enter: detail  r: refresh  q: quit"))

	return b.String()
}

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

func tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
