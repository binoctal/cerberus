package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RegressionTest represents a regression test case
type RegressionTest struct {
	ID              int
	Name            string
	BugID           sql.NullString // Can be NULL
	Category        string // complexity/abstraction/solid
	TestType        string // positive/negative
	Description     sql.NullString // Can be NULL
	FilePath        sql.NullString // Can be NULL
	InterfaceName   sql.NullString // Can be NULL
	ExpectedResult  string
	ActualResult    sql.NullString // Can be NULL
	Status          string // pending/pass/fail/skip
	LastRun         sql.NullTime // Can be NULL
	LastError       sql.NullString // Can be NULL
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Notes           sql.NullString // Can be NULL
}

// KnownIssue represents a known issue or false positive
type KnownIssue struct {
	ID               int
	IssueType        string // over_engineering/false_positive
	FilePath         string
	LineNumber       int
	Description      string
	IsFalsePositive  bool
	VerifiedBy       sql.NullString // Can be NULL
	VerifiedAt       sql.NullTime // Can be NULL
	VerificationNotes sql.NullString // Can be NULL
	RelatedBugID     sql.NullString // Can be NULL
	FixCommit       sql.NullString // Can be NULL
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AccuracyReport represents accuracy tracking data
type AccuracyReport struct {
	ID                int
	RunID             string
	Timestamp         time.Time
	ProjectPath       string
	TotalIssues       int
	TruePositives     int
	FalsePositives    int
	TrueNegatives     int
	ComplexityAcc     sql.NullFloat64 // Can be NULL
	AbstractionAcc    sql.NullFloat64 // Can be NULL
	SolidAcc          sql.NullFloat64 // Can be NULL
	OverallAccuracy   float64
	AnalyzerVersion   sql.NullString // Can be NULL
	CreatedAt         time.Time
}

// BugRecord represents a bug tracking record
type BugRecord struct {
	ID                 int
	BugID              string
	Title              string
	Description        string
	Severity           string
	Category           string
	AffectedComponent  string
	Status             string
	FixedInVersion     string
	RootCause          string
	RegressionTestID   int
	ReportedAt         time.Time
	FixedAt            time.Time
	CreatedAt          time.Time
}

// RegressionStore handles regression test operations
type RegressionStore struct {
	store *Store
}

// NewRegressionStore creates a new regression store
func NewRegressionStore(store *Store) *RegressionStore {
	return &RegressionStore{store: store}
}

// CreateRegressionTest creates a new regression test
func (r *RegressionStore) CreateRegressionTest(ctx context.Context, test *RegressionTest) (int, error) {
	result, err := r.store.DB().ExecContext(ctx, `
		INSERT INTO regression_tests (
			name, bug_id, category, test_type, description,
			file_path, interface_name, expected_result,
			notes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, test.Name, nullString(test.BugID), test.Category, test.TestType,
		nullString(test.Description), nullString(test.FilePath), nullString(test.InterfaceName),
		test.ExpectedResult, nullString(test.Notes))

	if err != nil {
		return 0, fmt.Errorf("create regression test: %w", err)
	}

	id, err := result.LastInsertId()
	return int(id), nil
}

// Helper function to convert sql.NullString to interface{}
func nullString(ns sql.NullString) interface{} {
	if ns.Valid {
		return ns.String
	}
	return nil
}

// GetRegressionTest retrieves a regression test by ID
func (r *RegressionStore) GetRegressionTest(ctx context.Context, id int) (*RegressionTest, error) {
	var test RegressionTest
	err := r.store.DB().QueryRowContext(ctx, `
		SELECT id, name, bug_id, category, test_type, description,
			   file_path, interface_name, expected_result, actual_result,
			   status, last_run, last_error, created_at, updated_at, notes
		FROM regression_tests WHERE id = ?
	`, id).Scan(
		&test.ID, &test.Name, &test.BugID, &test.Category, &test.TestType,
		&test.Description, &test.FilePath, &test.InterfaceName,
		&test.ExpectedResult, &test.ActualResult, &test.Status,
		&test.LastRun, &test.LastError, &test.CreatedAt, &test.UpdatedAt,
		&test.Notes,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("regression test not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get regression test: %w", err)
	}

	return &test, nil
}

// ListRegressionTests retrieves all regression tests, optionally filtered
func (r *RegressionStore) ListRegressionTests(ctx context.Context, category, status string) ([]*RegressionTest, error) {
	query := `
		SELECT id, name, bug_id, category, test_type, description,
			   file_path, interface_name, expected_result, actual_result,
			   status, last_run, last_error, created_at, updated_at, notes
		FROM regression_tests WHERE 1=1
	`
	args := []interface{}{}

	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list regression tests: %w", err)
	}
	defer rows.Close()

	var tests []*RegressionTest
	for rows.Next() {
		var test RegressionTest
		err := rows.Scan(
			&test.ID, &test.Name, &test.BugID, &test.Category, &test.TestType,
			&test.Description, &test.FilePath, &test.InterfaceName,
			&test.ExpectedResult, &test.ActualResult, &test.Status,
			&test.LastRun, &test.LastError, &test.CreatedAt, &test.UpdatedAt,
			&test.Notes,
		)
		if err != nil {
			return nil, fmt.Errorf("scan regression test: %w", err)
		}
		tests = append(tests, &test)
	}

	return tests, nil
}

// UpdateRegressionTestResult updates the result of a regression test
func (r *RegressionStore) UpdateRegressionTestResult(ctx context.Context, id int, actualResult, status, lastError string) error {
	_, err := r.store.DB().ExecContext(ctx, `
		UPDATE regression_tests
		SET actual_result = ?, status = ?, last_error = ?, last_run = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, actualResult, status, lastError, time.Now(), id)

	if err != nil {
		return fmt.Errorf("update regression test result: %w", err)
	}

	return nil
}

// RecordKnownIssue records a known issue or false positive
func (r *RegressionStore) RecordKnownIssue(ctx context.Context, issue *KnownIssue) error {
	_, err := r.store.DB().ExecContext(ctx, `
		INSERT INTO known_issues (
			issue_type, file_path, line_number, description,
			is_false_positive, verified_by, verified_at, verification_notes,
			related_bug_id, fix_commit
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, issue.IssueType, issue.FilePath, issue.LineNumber, issue.Description,
		issue.IsFalsePositive, nullString(issue.VerifiedBy), nullTime(issue.VerifiedAt),
		nullString(issue.VerificationNotes), nullString(issue.RelatedBugID), nullString(issue.FixCommit))

	if err != nil {
		return fmt.Errorf("record known issue: %w", err)
	}

	return nil
}

// Helper function to convert sql.NullTime to interface{}
func nullTime(nt sql.NullTime) interface{} {
	if nt.Valid {
		return nt.Time
	}
	return nil
}

// ListKnownIssues retrieves known issues, optionally filtered
func (r *RegressionStore) ListKnownIssues(ctx context.Context, issueType string, falsePositive *bool) ([]*KnownIssue, error) {
	query := `
		SELECT id, issue_type, file_path, line_number, description,
			   is_false_positive, verified_by, verified_at, verification_notes,
			   related_bug_id, fix_commit, created_at, updated_at
		FROM known_issues WHERE 1=1
	`
	args := []interface{}{}

	if issueType != "" {
		query += " AND issue_type = ?"
		args = append(args, issueType)
	}
	if falsePositive != nil {
		query += " AND is_false_positive = ?"
		args = append(args, *falsePositive)
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list known issues: %w", err)
	}
	defer rows.Close()

	var issues []*KnownIssue
	for rows.Next() {
		var issue KnownIssue
		err := rows.Scan(
			&issue.ID, &issue.IssueType, &issue.FilePath, &issue.LineNumber,
			&issue.Description, &issue.IsFalsePositive, &issue.VerifiedBy,
			&issue.VerifiedAt, &issue.VerificationNotes, &issue.RelatedBugID,
			&issue.FixCommit, &issue.CreatedAt, &issue.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan known issue: %w", err)
		}
		issues = append(issues, &issue)
	}

	return issues, nil
}

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

// Helper function to convert sql.NullFloat64 to interface{}
func nullFloat64(nf sql.NullFloat64) interface{} {
	if nf.Valid {
		return nf.Float64
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
	defer rows.Close()

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

// CreateBugRecord creates a new bug record
func (r *RegressionStore) CreateBugRecord(ctx context.Context, bug *BugRecord) error {
	_, err := r.store.DB().ExecContext(ctx, `
		INSERT INTO bug_tracker (
			bug_id, title, description, severity, category,
			affected_component, status, fixed_in_version,
			root_cause, regression_test_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, bug.BugID, bug.Title, bug.Description, bug.Severity, bug.Category,
		bug.AffectedComponent, bug.Status, bug.FixedInVersion,
		bug.RootCause, bug.RegressionTestID)

	if err != nil {
		return fmt.Errorf("create bug record: %w", err)
	}

	return nil
}
