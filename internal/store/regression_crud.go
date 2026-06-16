package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

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
