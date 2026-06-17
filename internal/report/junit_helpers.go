package report

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/store"
)

// evidenceSummary builds a compact text summary of evidence for a trace.
func evidenceSummary(evidence map[int64][]store.Evidence, traceID int64) string {
	evs, ok := evidence[traceID]
	if !ok || len(evs) == 0 {
		return ""
	}
	var b strings.Builder
	for i, ev := range evs {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s] %s", ev.Type, truncate(ev.Content, 200))
	}
	return b.String()
}

// verdictName builds a human-readable test case name from a verdict.
func verdictName(v store.Verdict) string {
	name := v.Target
	if name == "" {
		name = fmt.Sprintf("verdict-%d", v.ID)
	}
	return strings.NewReplacer(" ", "_", "/", ".", ":", "_").Replace(name)
}

// truncate shortens a string to maxLen characters with a truncation indicator.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}
