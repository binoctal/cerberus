package store

import (
	"context"
	"fmt"
)

// TrackAccuracy records an accuracy report
func (r *RegressionStore) TrackAccuracy(ctx context.Context, report *AccuracyReport) error {
	_, err := r.store.DB().ExecContext(ctx, `
		INSERT INTO accuracy_reports (
			run_id, timestamp, project_path,
			total_issues, true_positives, false_positives, true_negatives,
			overall_accuracy, complexity_accuracy,
			abstraction_accuracy, solid_accuracy, analyzer_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, report.RunID, report.Timestamp, report.ProjectPath,
		report.TotalIssues, report.TruePositives, report.FalsePositives,
		report.TrueNegatives, report.OverallAccuracy, nullFloat64(report.ComplexityAcc),
		nullFloat64(report.AbstractionAcc), nullFloat64(report.SolidAcc), nullString(report.AnalyzerVersion))

	if err != nil {
		return fmt.Errorf("track accuracy: %w", err)
	}

	return nil
}

// GetAccuracyHistory retrieves accuracy history
func (r *RegressionStore) GetAccuracyHistory(ctx context.Context, limit int) ([]*AccuracyReport, error) {
	rows, err := r.store.DB().QueryContext(ctx, `
		SELECT id, run_id, timestamp, project_path,
			   total_issues, true_positives, false_positives, true_negatives,
			   overall_accuracy, complexity_accuracy,
			   abstraction_accuracy, solid_accuracy, analyzer_version
		FROM accuracy_reports
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)

	if err != nil {
		return nil, fmt.Errorf("get accuracy history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var reports []*AccuracyReport
	for rows.Next() {
		var report AccuracyReport
		err := rows.Scan(
			&report.ID, &report.RunID, &report.Timestamp, &report.ProjectPath,
			&report.TotalIssues, &report.TruePositives, &report.FalsePositives,
			&report.TrueNegatives, &report.OverallAccuracy, &report.ComplexityAcc,
			&report.AbstractionAcc, &report.SolidAcc, &report.AnalyzerVersion,
		)
		if err != nil {
			return nil, fmt.Errorf("scan accuracy report: %w", err)
		}
		reports = append(reports, &report)
	}

	return reports, nil
}
