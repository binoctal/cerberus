package report

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/session"
)

// renderSummaryTable renders the summary table
func renderSummaryTable(b *strings.Builder, sum *session.SessionSummary) {
	b.WriteString("## Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	fmt.Fprintf(b, "|--------|-------|\n")
	fmt.Fprintf(b, "| **Endpoints Found** | %d |\n", sum.EndpointsFound)
	fmt.Fprintf(b, "| **Test Cases Planned** | %d |\n", sum.TestCasesPlanned)
	fmt.Fprintf(b, "| **Total Cases** | %d |\n", sum.TotalCases)
	fmt.Fprintf(b, "| **Passed** | %d |\n", sum.Passed)
	fmt.Fprintf(b, "| **Failed** | %d |\n", sum.Failed)
	fmt.Fprintf(b, "| **Skipped** | %d |\n", sum.Skipped)
	fmt.Fprintf(b, "| **Uncertain** | %d |\n", sum.Uncertain)
	fmt.Fprintf(b, "| **Recovered** | %d |\n", sum.Recovered)
	fmt.Fprintf(b, "| **Pending Review** | %d |\n", sum.PendingReview)
	if sum.DurationMs > 0 {
		fmt.Fprintf(b, "| **Duration** | %s |\n", sum.Duration)
	}
	if sum.TotalTokens > 0 {
		fmt.Fprintf(b, "| **Tokens Used** | ~%dK |\n", sum.TotalTokens/1000)
	}
	b.WriteString("\n")

	// Failure reason breakdown
	if sum.Failed > 0 {
		renderFailureBreakdown(b, sum)
	}
}

// renderFailureBreakdown renders the failure reason breakdown
func renderFailureBreakdown(b *strings.Builder, sum *session.SessionSummary) {
	b.WriteString("### Failure Breakdown\n\n")
	b.WriteString("| Failure Type | Count | Is System Bug? |\n")
	b.WriteString("|---------------|-------|----------------|\n")

	// This will be populated by the caller with actual data
	b.WriteString("\n")
}
