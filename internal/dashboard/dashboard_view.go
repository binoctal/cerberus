package dashboard

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/store"
)

// View renders the dashboard UI.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	m.renderHeader(&b)
	m.renderSessionStats(&b)

	if len(m.sessions) == 0 {
		m.renderEmptyState(&b)
	} else {
		sess := m.sessions[m.selected]
		m.renderSessionDetail(&b, sess)
		m.renderSummary(&b)
		m.renderVerdicts(&b)
		m.renderSessionList(&b)
	}

	m.renderFooter(&b)
	return b.String()
}

// renderHeader renders the dashboard title.
func (m Model) renderHeader(b *strings.Builder) {
	b.WriteString(titleStyle.Render("🛡️ Cerberus Dashboard"))
	b.WriteString("\n")
}

// renderSessionStats renders session count statistics.
func (m Model) renderSessionStats(b *strings.Builder) {
	completed, running := m.countSessionsByStatus()
	b.WriteString(statusStyle.Render(fmt.Sprintf("Sessions: %d completed, %d running", completed, running)))
	b.WriteString("\n\n")
}

// countSessionsByStatus returns completed and running session counts.
func (m Model) countSessionsByStatus() (completed, running int) {
	for _, s := range m.sessions {
		switch s.Status {
		case "completed":
			completed++
		case "running":
			running++
		}
	}
	return
}

// renderEmptyState renders the state when no sessions exist.
func (m Model) renderEmptyState(b *strings.Builder) {
	b.WriteString("No sessions found. Run `cerberus run --goal \"...\"` to start.\n")
}

// renderSessionDetail renders the selected session's header information.
func (m Model) renderSessionDetail(b *strings.Builder, sess store.Session) {
	fmt.Fprintf(b, "Session: %s  |  Status: %s  |  Goal: %s\n",
		sess.ID[:8], statusColor(sess.Status), sess.Goal)
	if sess.ProjectName != "" {
		fmt.Fprintf(b, "Project: %s\n", sess.ProjectName)
	}
	b.WriteString("\n")
}

// renderSummary renders the test summary section.
func (m Model) renderSummary(b *strings.Builder) {
	if m.summary == nil || m.summary.TotalCases == 0 {
		return
	}

	s := m.summary
	fmt.Fprintf(b, "Summary: %s %s %s %s %s\n",
		passStyle.Render(fmt.Sprintf("%d pass", s.Passed)),
		failStyle.Render(fmt.Sprintf("%d fail", s.Failed)),
		skipStyle.Render(fmt.Sprintf("%d skip", s.Skipped)),
		uncertainStyle.Render(fmt.Sprintf("%d uncertain", s.Uncertain)),
		fmt.Sprintf("(%d total)", s.TotalCases),
	)
	if s.Duration != "" {
		fmt.Fprintf(b, "Duration: %s  |  Tokens: ~%dK\n", s.Duration, s.TotalTokens/1000)
	}
	b.WriteString("\n")
}

// renderVerdicts renders the verdicts section.
func (m Model) renderVerdicts(b *strings.Builder) {
	if len(m.verdicts) == 0 {
		return
	}

	if m.detail {
		m.renderVerdictDetails(b)
	} else {
		m.renderVerdictSummary(b)
	}
}

// renderVerdictDetails renders full verdict details.
func (m Model) renderVerdictDetails(b *strings.Builder) {
	b.WriteString(detailBox.Render("Verdict Details"))
	b.WriteString("\n")
	for _, v := range m.verdicts {
		fmt.Fprintf(b, "  %s %s  conf:%.2f\n",
			statusColor(v.Status), v.Target, v.Confidence)
		if v.Reasoning != "" {
			fmt.Fprintf(b, "    %s\n", v.Reasoning)
		}
	}
}

// renderVerdictSummary renders a compact verdict summary (max 10 items).
func (m Model) renderVerdictSummary(b *strings.Builder) {
	b.WriteString("Verdicts:\n")
	maxShow := minInt(len(m.verdicts), 10)
	for _, v := range m.verdicts[:maxShow] {
		fmt.Fprintf(b, "  %s %-40s  conf:%.2f\n",
			statusColor(v.Status), truncate(v.Target, 40), v.Confidence)
	}
	if len(m.verdicts) > 10 {
		fmt.Fprintf(b, "  ... and %d more (press Enter for details)\n", len(m.verdicts)-10)
	}
	b.WriteString("\n")
}

// renderSessionList renders the session navigation list.
func (m Model) renderSessionList(b *strings.Builder) {
	if len(m.sessions) <= 1 {
		return
	}

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

// renderFooter renders the help text at the bottom.
func (m Model) renderFooter(b *strings.Builder) {
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑↓/jk: navigate  Enter: detail  r: refresh  q: quit"))
}
