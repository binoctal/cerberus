package main

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/architecture"
	"github.com/binoctal/cerberus/internal/store"
)

func runAbstractionTest(ctx context.Context, test *store.RegressionTest, verbose bool) (result, status, errorMsg string) {
	analyzer := architecture.NewAnalyzer(".")
	report, err := analyzer.Analyze()
	if err != nil {
		return "", "fail", fmt.Sprintf("分析失败: %v", err)
	}

	expectedIssueFound := false
	for _, issue := range report.Issues {
		if test.InterfaceName.Valid && test.InterfaceName.String != "" {
			if issue.Type == architecture.PrematureAbstraction {
				if test.TestType == "positive" {
					expectedIssueFound = true
					if verbose {
						fmt.Printf("  检测到抽象问题: %s\n", issue.Description)
					}
					break
				}
			}
		}
	}

	if test.TestType == "positive" {
		if expectedIssueFound {
			return "detected_abstraction_issue", "pass", ""
		} else {
			return "no_abstraction_issue", "fail", "应该检测到抽象问题但没有找到"
		}
	} else {
		if !expectedIssueFound {
			return "no_abstraction_issue", "pass", ""
		} else {
			return "detected_abstraction_issue", "fail", "不应该检测到抽象问题但找到了"
		}
	}
}
