package report

import (
	"github.com/binoctal/cerberus/internal/store"
)

// countFailuresByReason counts failed verdicts by their failure reason.
func countFailuresByReason(data *ReportData) []FailureInfo {
	counts := make(map[store.FailureReason]int)

	for _, v := range data.Verdicts {
		if v.Status == "fail" || v.Status == "failed" {
			reason := v.FailureReason
			if reason == "" {
				reason = store.FailureReasonSystemError // Default to system error if not specified
			}
			counts[reason]++
		}
	}

	// Convert to slice and sort by count (descending)
	var result []FailureInfo
	for reason, count := range counts {
		result = append(result, FailureInfo{Reason: reason, Count: count})
	}

	// Sort by count descending
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Count > result[i].Count {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

