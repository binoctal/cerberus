package report

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/session"
)

// renderFailureBreakdownWithCounts renders failure breakdown with actual counts
func renderFailureBreakdownWithCounts(b *strings.Builder, data *ReportData, sum *session.SessionSummary) {
	b.WriteString("### Failure Breakdown\n\n")
	b.WriteString("| Failure Type | Count | Is System Bug? |\n")
	b.WriteString("|---------------|-------|----------------|\n")

	// Count failures by reason
	failCountByReason := countFailuresByReason(data)
	totalSystemBugs := 0

	for _, failInfo := range failCountByReason {
		isBug := "❌ No"
		if failInfo.Reason.IsSystemBug() {
			isBug = "✅ Yes"
			totalSystemBugs += failInfo.Count
		}
		fmt.Fprintf(b, "| **%s** | %d | %s |\n",
			failInfo.Reason.DisplayName(), failInfo.Count, isBug)
	}

	b.WriteString("\n")
	renderFailureSummary(b, totalSystemBugs, sum.Failed)
}

// renderFailureSummary renders the failure summary message
func renderFailureSummary(b *strings.Builder, totalSystemBugs, totalFailed int) {
	if totalSystemBugs == 0 && totalFailed > 0 {
		b.WriteString("🎉 **Good News:** None of the failures are system bugs! ")
		b.WriteString("Most failures are due to LLM quality issues or expected policy rejections.\n\n")
	} else if totalSystemBugs > 0 {
		b.WriteString(fmt.Sprintf("⚠️ **Attention:** %d failure(s) appear to be genuine system bugs requiring investigation.\n\n", totalSystemBugs))
	}
}
