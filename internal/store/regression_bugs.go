package store

import (
	"context"
	"fmt"
)

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
