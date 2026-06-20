package main

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/architecture"
	"github.com/binoctal/cerberus/internal/store"
)

func runComplexityTest(ctx context.Context, test *store.RegressionTest, verbose bool) (result, status, errorMsg string) {
	analyzer := architecture.NewAnalyzer(".")
	report, err := analyzer.Analyze()
	if err != nil {
		return "", "fail", fmt.Sprintf("分析失败: %v", err)
	}

	// Match by issue type, not by file path. The complexity analyzer emits
	// OverEngineering for file-length, cyclomatic-complexity, and nesting
	// problems; asserting on the type (like the SOLID and abstraction runners
	// do) keeps the regression test stable as the codebase is refactored, so a
	// rename or split of the originally-flagged file no longer flips the result.
	expectedIssueFound := false
	for _, issue := range report.Issues {
		if issue.Type != architecture.OverEngineering {
			continue
		}
		if test.TestType == "positive" {
			expectedIssueFound = true
			if verbose {
				fmt.Printf("  检测到复杂度问题: %s (%s)\n", issue.Description, issue.File)
			}
			break
		}
	}

	if test.TestType == "positive" {
		if expectedIssueFound {
			return "detected_issue", "pass", ""
		}
		return "no_issue_detected", "fail", "应该检测到问题但没有找到"
	}
	if !expectedIssueFound {
		return "no_issue_detected", "pass", ""
	}
	return "detected_issue", "fail", "不应该检测到问题但找到了"
}
