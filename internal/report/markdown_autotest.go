package report

import (
	"fmt"
	"path/filepath"
	"strings"
)

// renderAutoTest renders the AutoTest section if present.
func renderAutoTest(data *ReportData) string {
	if data.AutoTest == nil || len(data.AutoTest.Items) == 0 {
		return ""
	}

	var b strings.Builder

	// Section header
	b.WriteString("## AutoTest\n\n")

	// Coverage summary
	b.WriteString(fmt.Sprintf("| Before → After Coverage | %.1f%% → %.1f%% |\n",
		data.AutoTest.BeforeCoveragePct, data.AutoTest.AfterCoveragePct))

	// Status summary
	written := countStatus(data, "written")
	reverted := countStatus(data, "reverted")
	skipped := countStatus(data, "skipped")
	failed := countStatus(data, "failed")
	b.WriteString(fmt.Sprintf("| Written / Reverted / Skipped / Failed | %d / %d / %d / %d |\n\n",
		written, reverted, skipped, failed))

	// Item table
	b.WriteString("| # | Test File | Target | Reason | Status |\n")
	b.WriteString("|---|-----------|--------|--------|--------|\n")

	for i, item := range data.AutoTest.Items {
		statusBadge := statusBadge(item.Status)
		b.WriteString(fmt.Sprintf("| %d | `%s` | %s (`%s`) | %s | %s |\n",
			i+1,
			item.TestPath,
			item.TargetFunc,
			filepath.Base(item.TargetFile),
			item.Reason,
			statusBadge))
	}
	b.WriteString("\n")

	return b.String()
}
