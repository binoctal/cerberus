package store

import (
	"context"
	"fmt"
)

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
