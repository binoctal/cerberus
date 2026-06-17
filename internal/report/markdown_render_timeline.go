package report

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/store"
)

// renderTimelineSection renders the execution timeline
func renderTimelineSection(b *strings.Builder, traces []store.Trace) {
	if len(traces) == 0 {
		return
	}

	b.WriteString("## Execution Timeline\n\n")
	b.WriteString("| # | Category | Target | Status | Started |\n")
	b.WriteString("|---|----------|--------|--------|----------|\n")
	for i, t := range traces {
		fmt.Fprintf(b, "| %d | %s | `%s` | %s | %s |\n",
			i+1, t.Category, t.Target, statusEmoji(t.Status), t.StartedAt)
	}
	b.WriteString("\n")
}
