package report

import (
	"html/template"
	"strings"

	"github.com/binoctal/cerberus/internal/store"
)

// htmlTmpl is the parsed HTML template with custom functions.
var htmlTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"add":                 func(a, b int) int { return a + b },
	"truncate":            truncate,
	"countStatus":         countStatusInItems,
	"baseName":            baseName,
	"indexFailureReasons": indexFailureReasonsInHTML,
}).Parse(htmlTemplate))

// countStatusInItems counts items with a given status (for HTML template).
func countStatusInItems(items interface{}, status string) int {
	// Use type assertion to handle the autotest.AutoTestItem slice
	count := 0
	if itemsSlice, ok := items.([]interface{}); ok {
		for _, item := range itemsSlice {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if s, ok := itemMap["Status"].(string); ok && s == status {
					count++
				}
			}
		}
	}
	return count
}

// baseName extracts the base name from a file path (for HTML template).
func baseName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx != -1 {
		return path[idx+1:]
	}
	if idx := strings.LastIndex(path, "\\"); idx != -1 {
		return path[idx+1:]
	}
	return path
}

// indexFailureReasonsInHTML counts failed verdicts by their failure reason (for HTML template).
// Returns a slice of FailureInfo sorted by count (descending).
func indexFailureReasonsInHTML(data interface{}) []FailureInfo {
	var result []FailureInfo

	reportData, ok := data.(*ReportData)
	if !ok {
		return result
	}

	// Count failures by reason
	counts := make(map[store.FailureReason]int)
	for _, v := range reportData.Verdicts {
		if v.Status == "fail" || v.Status == "failed" {
			reason := v.FailureReason
			if reason == "" {
				reason = store.FailureReasonSystemError // Default to system error if not specified
			}
			counts[reason]++
		}
	}

	// Convert to slice and sort by count (descending)
	for reason, count := range counts {
		result = append(result, FailureInfo{
			Reason: reason,
			Count:  count,
		})
	}

	// Sort by count descending (bubble sort for simplicity)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Count > result[i].Count {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}
