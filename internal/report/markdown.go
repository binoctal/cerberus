package report

import (
	"fmt"
	"strings"
)

// RenderMarkdown returns a Markdown report for the given data.
func RenderMarkdown(data *ReportData) string {
	var b strings.Builder
	s := data.Session

	b.WriteString("# Cerberus Test Report\n\n")

	// Session metadata.
	b.WriteString("## Session\n\n")
	b.WriteString("| Field | Value |\n")
	b.WriteString("|-------|-------|\n")
	fmt.Fprintf(&b, "| **ID** | `%s` |\n", s.ID)
	fmt.Fprintf(&b, "| **Goal** | %s |\n", s.Goal)
	fmt.Fprintf(&b, "| **Status** | %s |\n", statusEmoji(s.Status))
	if s.ProjectName != "" {
		fmt.Fprintf(&b, "| **Project** | %s |\n", s.ProjectName)
	}
	fmt.Fprintf(&b, "| **Started** | %s |\n", s.StartedAt)
	if s.FinishedAt != "" {
		fmt.Fprintf(&b, "| **Finished** | %s |\n", s.FinishedAt)
	}
	b.WriteString("\n")

	// Summary table.
	if data.Summary != nil && data.Summary.TotalCases > 0 {
		sum := data.Summary
		b.WriteString("## Summary\n\n")
		b.WriteString("| Metric | Value |\n")
		fmt.Fprintf(&b, "|--------|-------|\n")
		fmt.Fprintf(&b, "| **Endpoints Found** | %d |\n", sum.EndpointsFound)
		fmt.Fprintf(&b, "| **Test Cases Planned** | %d |\n", sum.TestCasesPlanned)
		fmt.Fprintf(&b, "| **Total Cases** | %d |\n", sum.TotalCases)
		fmt.Fprintf(&b, "| **Passed** | %d |\n", sum.Passed)
		fmt.Fprintf(&b, "| **Failed** | %d |\n", sum.Failed)
		fmt.Fprintf(&b, "| **Skipped** | %d |\n", sum.Skipped)
		fmt.Fprintf(&b, "| **Uncertain** | %d |\n", sum.Uncertain)
		fmt.Fprintf(&b, "| **Pending Review** | %d |\n", sum.PendingReview)
		if sum.DurationMs > 0 {
			fmt.Fprintf(&b, "| **Duration** | %s |\n", sum.Duration)
		}
		if sum.TotalTokens > 0 {
			fmt.Fprintf(&b, "| **Tokens Used** | ~%dK |\n", sum.TotalTokens/1000)
		}
		b.WriteString("\n")
	}

	// Verdicts table.
	if len(data.Verdicts) > 0 {
		b.WriteString("## Verdicts\n\n")
		b.WriteString("| # | Target | Status | Confidence | Source |\n")
		b.WriteString("|---|--------|--------|------------|--------|\n")
		for i, v := range data.Verdicts {
			fmt.Fprintf(&b, "| %d | `%s` | %s | %.2f | %s |\n",
				i+1, v.Target, statusEmoji(v.Status), v.Confidence, v.Source)
		}
		b.WriteString("\n")

		// Verdict details.
		hasDetails := false
		for _, v := range data.Verdicts {
			if v.Reasoning != "" {
				hasDetails = true
				break
			}
		}
		if hasDetails {
			b.WriteString("### Verdict Details\n\n")
			for _, v := range data.Verdicts {
				if v.Reasoning == "" {
					continue
				}
				fmt.Fprintf(&b, "**%s** (%s):\n\n", v.Target, v.Status)
				fmt.Fprintf(&b, "> %s\n\n", v.Reasoning)
			}
		}
	}

	// Evidence section.
	if len(data.Evidence) > 0 {
		b.WriteString("## Evidence\n\n")
		for _, v := range data.Verdicts {
			evs, ok := data.Evidence[v.TraceID]
			if !ok || len(evs) == 0 {
				continue
			}
			fmt.Fprintf(&b, "<details>\n<summary>%s (%d evidence)</summary>\n\n", v.Target, len(evs))
			for _, ev := range evs {
				fmt.Fprintf(&b, "**[%s]** %s\n\n", ev.Type, truncate(ev.Content, 500))
			}
			b.WriteString("</details>\n\n")
		}
		// Also show evidence for traces without verdicts.
		shownTraces := make(map[int64]bool)
		for _, v := range data.Verdicts {
			shownTraces[v.TraceID] = true
		}
		for _, tr := range data.Traces {
			if shownTraces[tr.ID] {
				continue
			}
			evs, ok := data.Evidence[tr.ID]
			if !ok || len(evs) == 0 {
				continue
			}
			fmt.Fprintf(&b, "<details>\n<summary>%s (%d evidence)</summary>\n\n", tr.Target, len(evs))
			for _, ev := range evs {
				fmt.Fprintf(&b, "**[%s]** %s\n\n", ev.Type, truncate(ev.Content, 500))
			}
			b.WriteString("</details>\n\n")
		}
	}

	// Traces timeline.
	if len(data.Traces) > 0 {
		b.WriteString("## Execution Timeline\n\n")
		b.WriteString("| # | Category | Target | Status | Started |\n")
		b.WriteString("|---|----------|--------|--------|----------|\n")
		for i, t := range data.Traces {
			fmt.Fprintf(&b, "| %d | %s | `%s` | %s | %s |\n",
				i+1, t.Category, t.Target, statusEmoji(t.Status), t.StartedAt)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// statusEmoji returns a human-friendly status with emoji prefix.
func statusEmoji(status string) string {
	switch status {
	case "pass", "passed", "completed":
		return "✅ " + status
	case "fail", "failed":
		return "❌ " + status
	case "uncertain":
		return "⚠️ " + status
	case "skip", "skipped":
		return "⏭️ " + status
	case "running":
		return "🔄 " + status
	case "aborted":
		return "🛑 " + status
	default:
		return status
	}
}
