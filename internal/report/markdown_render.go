package report

import (
	"strings"
)

// RenderMarkdown returns a Markdown report for the given data.
func RenderMarkdown(data *ReportData) string {
	var b strings.Builder

	b.WriteString("# Cerberus Test Report\n\n")

	// Phase 1: Session metadata
	renderSessionMetadata(&b, data.Session)

	// Phase 2: Summary table
	if data.Summary != nil && data.Summary.TotalCases > 0 {
		renderSummaryTable(&b, data.Summary)
		if data.Summary.Failed > 0 {
			renderFailureBreakdownWithCounts(&b, data, data.Summary)
		}
	}

	// Phase 3: Verdicts table
	renderVerdictsTable(&b, data.Verdicts)

	// Phase 4: Evidence section
	renderEvidenceSection(&b, data)

	// Phase 5: Execution timeline
	renderTimelineSection(&b, data.Traces)

	// Phase 6: AutoTest section
	b.WriteString(renderAutoTest(data))

	return b.String()
}
