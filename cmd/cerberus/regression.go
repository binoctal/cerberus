package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/architecture"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/store"
)

// Note: sql.NullFloat64 is available from the database/sql package

var (
	regressionCategoryFlag string
	regressionVerboseFlag   bool
	accuracyLimitFlag       int
)

func regressionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "regression",
		Short: "Run regression tests for architecture analyzer",
		Long:  "运行架构分析器的回归测试，确保已修复的bug不会重现",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer func() { _ = logger.Sync() }()

			s, err := store.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = s.Close() }()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			return runRegressionTests(ctx, s, logger, regressionCategoryFlag, regressionVerboseFlag)
		},
	}

	cmd.Flags().StringVarP(&regressionCategoryFlag, "category", "c", "", "测试类别 (complexity/abstraction/solid)")
	cmd.Flags().BoolVarP(&regressionVerboseFlag, "verbose", "v", false, "详细输出")

	return cmd
}

func accuracyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accuracy",
		Short: "Show accuracy reports and history",
		Long:  "显示架构分析器的准确率报告和历史数据",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer func() { _ = logger.Sync() }()

			s, err := store.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = s.Close() }()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			return showAccuracyReports(ctx, s, accuracyLimitFlag)
		},
	}

	cmd.Flags().IntVarP(&accuracyLimitFlag, "limit", "l", 10, "显示最近N条记录")

	return cmd
}

func knownIssueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "known-issue",
		Short: "Manage known issues and false positives",
		Long:  "管理已知问题和误报记录",
	}

	// List known issues
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List known issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer func() { _ = logger.Sync() }()

			s, err := store.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = s.Close() }()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			regStore := store.NewRegressionStore(s)
			issues, err := regStore.ListKnownIssues(ctx, "", nil)
			if err != nil {
				return fmt.Errorf("list known issues: %w", err)
			}

			fmt.Println("已知问题:")
			fmt.Println("=====================================")
			for _, issue := range issues {
				status := "✓ 真实问题"
				if issue.IsFalsePositive {
					status = "⊘ 误报"
				}
				fmt.Printf("%s [%s] %s:%d\n", status, issue.IssueType, issue.FilePath, issue.LineNumber)
				fmt.Printf("  %s\n", issue.Description)
				if issue.VerifiedBy.Valid && issue.VerifiedBy.String != "" {
					fmt.Printf("  验证者: %s", issue.VerifiedBy.String)
					if issue.VerifiedAt.Valid {
						fmt.Printf(" @ %s\n", issue.VerifiedAt.Time.Format("2006-01-02"))
					} else {
						fmt.Println()
					}
				}
				fmt.Println()
			}

			return nil
		},
	}

	// Add known issue
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a known issue",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Interactive prompt for adding known issues
			fmt.Println("添加已知问题 - 功能开发中")
			return nil
		},
	}

	cmd.AddCommand(listCmd, addCmd)
	return cmd
}

func runRegressionTests(ctx context.Context, s *store.Store, logger *zap.Logger, category string, verbose bool) error {
	regStore := store.NewRegressionStore(s)

	// Get all regression tests
	tests, err := regStore.ListRegressionTests(ctx, category, "")
	if err != nil {
		return fmt.Errorf("list regression tests: %w", err)
	}

	if len(tests) == 0 {
		fmt.Println("没有回归测试")
		fmt.Println("使用 'cerberus known-issue add' 添加已知问题后自动创建测试")
		return nil
	}

	fmt.Printf("运行 %d 个回归测试...\n\n", len(tests))

	passCount := 0
	failCount := 0
	skipCount := 0

	for _, test := range tests {
		fmt.Printf("[%s] %s\n", test.Category, test.Name)
		if test.Description.Valid && test.Description.String != "" {
			fmt.Printf("  描述: %s\n", test.Description.String)
		}

		// Run test based on category
		var result string
		var status string
		var errorMsg string

		switch test.Category {
		case "complexity":
			result, status, errorMsg = runComplexityTest(ctx, test, verbose)
		case "abstraction":
			result, status, errorMsg = runAbstractionTest(ctx, test, verbose)
		case "solid":
			result, status, errorMsg = runSOLIDTest(ctx, test, verbose)
		default:
			status = "skip"
			skipCount++
			fmt.Printf("  ⊘ 跳过 (未知类别)\n\n")
			continue
		}

		// Update test result
		if err := regStore.UpdateRegressionTestResult(ctx, test.ID, result, status, errorMsg); err != nil {
			logger.Warn("更新测试结果失败", zap.Error(err))
		}

		// Update counts
		switch status {
		case "pass":
			passCount++
			fmt.Printf("  ✓ 通过\n\n")
		case "fail":
			failCount++
			fmt.Printf("  ✗ 失败: %s\n\n", errorMsg)
		case "skip":
			skipCount++
			fmt.Printf("  ⊘ 跳过\n\n")
		}
	}

	// Print summary
	fmt.Println("=====================================")
	fmt.Printf("总计: %d 通过, %d 失败, %d 跳过\n", passCount, failCount, skipCount)

	if failCount > 0 {
		return fmt.Errorf("%d 个回归测试失败", failCount)
	}

	return nil
}

func runComplexityTest(ctx context.Context, test *store.RegressionTest, verbose bool) (result, status, errorMsg string) {
	// Run architecture analyzer on the test file
	analyzer := architecture.NewAnalyzer(".")
	report, err := analyzer.Analyze()
	if err != nil {
		return "", "fail", fmt.Sprintf("分析失败: %v", err)
	}

	// Check if expected issues were found
	expectedIssueFound := false
	for _, issue := range report.Issues {
		if test.FilePath.Valid && issue.File == test.FilePath.String {
			if test.TestType == "positive" {
				// Should detect an issue
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

	// Determine test result
	if test.TestType == "positive" {
		// Should detect issues
		if expectedIssueFound {
			return "detected_issue", "pass", ""
		} else {
			return "no_issue_detected", "fail", "应该检测到问题但没有找到"
		}
	} else {
		// Should NOT detect issues (negative test)
		if !expectedIssueFound {
			return "no_issue_detected", "pass", ""
		} else {
			return "detected_issue", "fail", "不应该检测到问题但找到了"
		}
	}
}

func runAbstractionTest(ctx context.Context, test *store.RegressionTest, verbose bool) (result, status, errorMsg string) {
	// Run architecture analyzer
	analyzer := architecture.NewAnalyzer(".")
	report, err := analyzer.Analyze()
	if err != nil {
		return "", "fail", fmt.Sprintf("分析失败: %v", err)
	}

	// Check abstraction-related issues
	expectedIssueFound := false
	for _, issue := range report.Issues {
		if test.InterfaceName.Valid && test.InterfaceName.String != "" {
			// Check if interface implementation was detected
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

	// Determine test result
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

func runSOLIDTest(ctx context.Context, test *store.RegressionTest, verbose bool) (result, status, errorMsg string) {
	// Run architecture analyzer
	analyzer := architecture.NewAnalyzer(".")
	report, err := analyzer.Analyze()
	if err != nil {
		return "", "fail", fmt.Sprintf("分析失败: %v", err)
	}

	// Check SOLID violations
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
				break
			}
		}
	}

	// Determine test result
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

func showAccuracyReports(ctx context.Context, s *store.Store, limit int) error {
	regStore := store.NewRegressionStore(s)

	reports, err := regStore.GetAccuracyHistory(ctx, limit)
	if err != nil {
		return fmt.Errorf("get accuracy history: %w", err)
	}

	if len(reports) == 0 {
		fmt.Println("没有准确率报告")
		fmt.Println("运行 'cerberus regression' 后会自动生成报告")
		return nil
	}

	fmt.Println("准确率历史:")
	fmt.Println("=====================================")
	for _, report := range reports {
		fmt.Printf("运行 ID: %s\n", report.RunID)
		fmt.Printf("时间: %s\n", report.Timestamp.Format("2006-01-02 15:04:05"))
		fmt.Printf("项目: %s\n", report.ProjectPath)
		fmt.Printf("总问题: %d | 真阳性: %d | 假阳性: %d | 真阴性: %d\n",
			report.TotalIssues, report.TruePositives, report.FalsePositives, report.TrueNegatives)
		fmt.Printf("总体准确率: %.1f%%\n", report.OverallAccuracy*100)

		if report.ComplexityAcc.Valid && report.ComplexityAcc.Float64 > 0 {
			fmt.Printf("  复杂度: %.1f%%", report.ComplexityAcc.Float64*100)
		}
		if report.AbstractionAcc.Valid && report.AbstractionAcc.Float64 > 0 {
			fmt.Printf("  抽象: %.1f%%", report.AbstractionAcc.Float64*100)
		}
		if report.SolidAcc.Valid && report.SolidAcc.Float64 > 0 {
			fmt.Printf("  SOLID: %.1f%%", report.SolidAcc.Float64*100)
		}
		fmt.Println()
		fmt.Println("-------------------------------------")
	}

	return nil
}

// initRegressionTests seeds initial regression tests if none exist
func initRegressionTests(ctx context.Context, s *store.Store) error {
	regStore := store.NewRegressionStore(s)

	// Check if tests already exist
	tests, err := regStore.ListRegressionTests(ctx, "", "")
	if err != nil {
		return err
	}

	if len(tests) > 0 {
		return nil // Already initialized
	}

	// Seed initial tests based on known issues
	issues, err := regStore.ListKnownIssues(ctx, "", nil)
	if err != nil {
		return err
	}

	for _, issue := range issues {
		// Create regression test from known issue
		testType := "positive"
		if issue.IsFalsePositive {
			testType = "negative"
		}

		test := &store.RegressionTest{
			Name:           fmt.Sprintf("Test-%s", issue.IssueType),
			BugID:          issue.RelatedBugID,
			Category:       "complexity", // Default
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
