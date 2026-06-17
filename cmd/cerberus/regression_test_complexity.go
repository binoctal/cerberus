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

	expectedIssueFound := false
	for _, issue := range report.Issues {
		if test.FilePath.Valid && issue.File == test.FilePath.String {
			if test.TestType == "positive" {
				if issue.Severity == architecture.SeverityError || issue.Severity == architecture.SeverityWarning {
					expectedIssueFound = true
					if verbose {
						fmt.Printf("  检测到问题: %s\n", issue.Description)
					}
					break
				}
			}
		}
	}

	if test.TestType == "positive" {
		if expectedIssueFound {
			return "detected_issue", "pass", ""
		} else {
			return "no_issue_detected", "fail", "应该检测到问题但没有找到"
		}
	} else {
		if !expectedIssueFound {
			return "no_issue_detected", "pass", ""
		} else {
			return "detected_issue", "fail", "不应该检测到问题但找到了"
		}
	}
}
