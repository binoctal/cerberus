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
	b.WriteString(fmt.Sprintf("| Field | Value |\n"))
	b.WriteString(fmt.Sprintf("|-------|-------|\n"))
	b.WriteString(fmt.Sprintf("| **ID** | `%s` |\n", s.ID))
	b.WriteString(fmt.Sprintf("| **Goal** | %s |\n", s.Goal))
	b.WriteString(fmt.Sprintf("| **Status** | %s |\n", statusEmoji(s.Status)))
	if s.ProjectName != "" {
		b.WriteString(fmt.Sprintf("| **Project** | %s |\n", s.ProjectName))
	}
	b.WriteString(fmt.Sprintf("| **Started** | %s |\n", s.StartedAt))
	if s.FinishedAt != "" {
		b.WriteString(fmt.Sprintf("| **Finished** | %s |\n", s.FinishedAt))
	}
	b.WriteString("\n")

	// Summary table.
	if data.Summary != nil && data.Summary.TotalCases > 0 {
		sum := data.Summary
		b.WriteString("## Summary\n\n")
		b.WriteString(fmt.Sprintf("| Metric | Value |\n"))
		b.WriteString(fmt.Sprintf("|--------|-------|\n"))
		b.WriteString(fmt.Sprintf("| **Endpoints Found** | %d |\n", sum.EndpointsFound))
		b.WriteString(fmt.Sprintf("| **Test Cases Planned** | %d |\n", sum.TestCasesPlanned))
		b.WriteString(fmt.Sprintf("| **Total Cases** | %d |\n", sum.TotalCases))
		b.WriteString(fmt.Sprintf("| **Passed** | %d |\n", sum.Passed))
		b.WriteString(fmt.Sprintf("| **Failed** | %d |\n", sum.Failed))
		b.WriteString(fmt.Sprintf("| **Skipped** | %d |\n", sum.Skipped))
		b.WriteString(fmt.Sprintf("| **Uncertain** | %d |\n", sum.Uncertain))
		b.WriteString(fmt.Sprintf("| **Pending Review** | %d |\n", sum.PendingReview))
		if sum.DurationMs > 0 {
			b.WriteString(fmt.Sprintf("| **Duration** | %s |\n", sum.Duration))
		}
		if sum.TotalTokens > 0 {
			b.WriteString(fmt.Sprintf("| **Tokens Used** | ~%dK |\n", sum.TotalTokens/1000))
		}
		b.WriteString("\n")
	}

	// Verdicts table.
	if len(data.Verdicts) > 0 {
		b.WriteString("## Verdicts\n\n")
		b.WriteString("| # | Target | Status | Confidence | Source |\n")
		b.WriteString("|---|--------|--------|------------|--------|\n")
		for i, v := range data.Verdicts {
			b.WriteString(fmt.Sprintf("| %d | `%s` | %s | %.2f | %s |\n",
				i+1, v.Target, statusEmoji(v.Status), v.Confidence, v.Source))
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
				b.WriteString(fmt.Sprintf("**%s** (%s):\n\n", v.Target, v.Status))
				b.WriteString(fmt.Sprintf("> %s\n\n", v.Reasoning))
			}
		}
	}

	// Traces timeline.
	if len(data.Traces) > 0 {
		b.WriteString("## Execution Timeline\n\n")
		b.WriteString("| # | Category | Target | Status | Started |\n")
		b.WriteString("|---|----------|--------|--------|----------|\n")
		for i, t := range data.Traces {
			b.WriteString(fmt.Sprintf("| %d | %s | `%s` | %s | %s |\n",
				i+1, t.Category, t.Target, statusEmoji(t.Status), t.StartedAt))
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
