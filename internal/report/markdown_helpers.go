package report

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
	case "recovered":
		return "♻️ recovered"
	case "running":
		return "🔄 " + status
	case "aborted":
		return "🛑 " + status
	default:
		return status
	}
}

// statusBadge returns a status string with emoji badge.
func statusBadge(status string) string {
	switch status {
	case "written":
		return "✅ written"
	case "reverted":
		return "❌ reverted"
	case "skipped":
		return "⏭️ skipped"
	case "failed":
		return "💥 failed"
	case "generated":
		return "📝 generated"
	default:
		return status
	}
}

// countStatus counts items with a given status.
func countStatus(data *ReportData, status string) int {
	count := 0
	if data.AutoTest != nil {
		for _, item := range data.AutoTest.Items {
			if item.Status == status {
				count++
			}
		}
	}
	return count
}
