package architecture

// calculateSummary calculates summary statistics
func (r *ArchitectureReport) calculateSummary() {
	if r.Summary == nil {
		r.Summary = &ReportSummary{
			CategoryScores: make(map[string]int),
		}
	}

	r.Summary.TotalIssues = len(r.Issues)

	for _, issue := range r.Issues {
		switch issue.Severity {
		case SeverityError:
			r.Summary.ErrorCount++
		case SeverityWarning:
			r.Summary.WarningCount++
		case SeverityInfo:
			r.Summary.InfoCount++
		}
	}
}
