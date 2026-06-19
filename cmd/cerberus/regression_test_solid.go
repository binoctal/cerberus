package main

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/architecture"
	"github.com/binoctal/cerberus/internal/store"
)

func runSOLIDTest(ctx context.Context, test *store.RegressionTest, verbose bool) (result, status, errorMsg string) {
	analyzer := architecture.NewAnalyzer(".")
	report, err := analyzer.Analyze()
	if err != nil {
		return "", "fail", fmt.Sprintf("分析失败: %v", err)
	}

	expectedViolationFound := false
	for _, issue := range report.Issues {
		switch issue.Type {
		case architecture.ViolatesSRP, architecture.ViolatesOCP,
			architecture.ViolatesLSP, architecture.ViolatesISP, architecture.ViolatesDIP:
			if test.TestType == "positive" {
				expectedViolationFound = true
				if verbose {
					fmt.Printf("  检测到SOLID违反: %s (%s)\n", issue.Description, issue.Type)
				}
			}
		}
	}

	if test.TestType == "positive" {
		if expectedViolationFound {
			return "detected_solid_violation", "pass", ""
		} else {
			return "no_solid_violation", "fail", "应该检测到SOLID违反但没有找到"
		}
	} else {
		if !expectedViolationFound {
			return "no_solid_violation", "pass", ""
		} else {
			return "detected_solid_violation", "fail", "不应该检测到SOLID违反但找到了"
		}
	}
}
