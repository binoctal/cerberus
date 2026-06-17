package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/binoctal/cerberus/internal/store"
)

func initRegressionTests(ctx context.Context, s *store.Store) error {
	regStore := store.NewRegressionStore(s)

	tests, err := regStore.ListRegressionTests(ctx, "", "")
	if err != nil {
		return err
	}

	if len(tests) > 0 {
		return nil
	}

	issues, err := regStore.ListKnownIssues(ctx, "", nil)
	if err != nil {
		return err
	}

	for _, issue := range issues {
		testType := "positive"
		if issue.IsFalsePositive {
			testType = "negative"
		}

		test := &store.RegressionTest{
			Name:           fmt.Sprintf("Test-%s", issue.IssueType),
			BugID:          issue.RelatedBugID,
			Category:       "complexity",
			TestType:       testType,
			Description:    sql.NullString{String: issue.Description, Valid: true},
			FilePath:       sql.NullString{String: issue.FilePath, Valid: issue.FilePath != ""},
			ExpectedResult: "verified_as_" + testType,
			Notes:          sql.NullString{String: "Auto-generated from known issue", Valid: true},
		}

		if _, err := regStore.CreateRegressionTest(ctx, test); err != nil {
			return err
		}
	}

	return nil
}
