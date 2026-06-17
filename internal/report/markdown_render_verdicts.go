package report

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/store"
)

// renderVerdictsTable renders the verdicts table and details
func renderVerdictsTable(b *strings.Builder, verdicts []store.Verdict) {
	if len(verdicts) == 0 {
		return
	}

	b.WriteString("## Verdicts\n\n")
	b.WriteString("| # | Target | Status | Confidence | Failure Reason | Source |\n")
	b.WriteString("|---|--------|--------|------------|----------------|--------|\n")
	for i, v := range verdicts {
		failReason := "—"
		if (v.Status == "fail" || v.Status == "failed") && v.FailureReason != "" {
			failReason = v.FailureReason.DisplayName()
		}
		fmt.Fprintf(b, "| %d | `%s` | %s | %.2f | %s | %s |\n",
			i+1, v.Target, statusEmoji(v.Status), v.Confidence, failReason, v.Source)
	}
	b.WriteString("\n")

	// Verdict details
	if hasVerdictDetails(verdicts) {
		renderVerdictDetails(b, verdicts)
	}
}

// hasVerdictDetails checks if any verdict has reasoning details
func hasVerdictDetails(verdicts []store.Verdict) bool {
	for _, v := range verdicts {
		if v.Reasoning != "" {
			return true
		}
	}
	return false
}

// renderVerdictDetails renders detailed reasoning for verdicts
func renderVerdictDetails(b *strings.Builder, verdicts []store.Verdict) {
	b.WriteString("### Verdict Details\n\n")
	for _, v := range verdicts {
		if v.Reasoning == "" {
			continue
		}
		fmt.Fprintf(b, "**%s** (%s):\n\n", v.Target, v.Status)
		fmt.Fprintf(b, "> %s\n\n", v.Reasoning)
	}
}
