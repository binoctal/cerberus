package dashboard

import (
	"fmt"
	"strings"
)

// View renders the dashboard UI.
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
