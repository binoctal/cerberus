package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/binoctal/cerberus/internal/architecture"
)

// runArchitectureCheck runs architecture quality check
func runArchitectureCheck(projectPath string) error {
	// Get absolute path
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if path exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	// Create analyzer
	analyzer := architecture.NewAnalyzer(absPath)

	// Run analysis
	report, err := analyzer.Analyze()
	if err != nil {
		return fmt.Errorf("architecture analysis failed: %w", err)
	}

	// Print report
	printArchitectureReport(report)

	return nil
}

// printArchitectureReport prints architecture analysis report
func printArchitectureReport(report *architecture.ArchitectureReport) {
	fmt.Println("🏗️  架构质量检查报告")
	fmt.Println()

	// Print metrics
	fmt.Println("📊 代码度量:")
	fmt.Printf("  总文件数: %d\n", report.Metrics.TotalFiles)
	fmt.Printf("  总代码行数: %d\n", report.Metrics.TotalLines)
	if report.Metrics.TotalFiles > 0 {
		avgLines := report.Metrics.TotalLines / report.Metrics.TotalFiles
		fmt.Printf("  平均行数/文件: %d\n", avgLines)
	}
	fmt.Printf("  最大行数/文件: %d\n", report.Metrics.MaxLinesPerFile)
	fmt.Println()

	// Print dependency metrics
	fmt.Println("📦 依赖度量:")
	fmt.Printf("  总依赖数: %d\n", report.Metrics.TotalDependencies)
	fmt.Printf("  循环依赖: %d\n", report.Metrics.CircularDependencies)
	fmt.Println()

	// Print abstraction metrics
	fmt.Println("🔧 抽象度量:")
	fmt.Printf("  总接口数: %d\n", report.Metrics.TotalInterfaces)
	fmt.Printf("  单一实现接口: %d\n", report.Metrics.SingleImplInterfaces)
	fmt.Printf("  未使用抽象: %d\n", report.Metrics.UnusedAbstractions)
	fmt.Println()

	// Print SOLID metrics
	fmt.Println("📐 SOLID 原则:")
	fmt.Printf("  SRP 违规: %d\n", report.Metrics.SRPViolations)
	fmt.Printf("  OCP 违规: %d\n", report.Metrics.OCPViolations)
	fmt.Println()

	// Print documentation metrics
	if report.Metrics.ADRFiles > 0 || report.Metrics.DesignDocs > 0 {
		fmt.Println("📚 文档度量:")
		fmt.Printf("  ADR 文件: %d\n", report.Metrics.ADRFiles)
		fmt.Printf("  设计文档: %d\n", report.Metrics.DesignDocs)
		fmt.Printf("  实现计划: %d\n", report.Metrics.PlanDocs)
		fmt.Println()
	}

	// Print issues
	if len(report.Issues) > 0 {
		fmt.Printf("⚠️  发现 %d 个架构问题:\n\n", len(report.Issues))

		for i, issue := range report.Issues {
			fmt.Printf("%d. [%s] %s (%s)\n", i+1, issue.Severity, issue.Type, issue.File)
			if issue.Line > 0 {
				fmt.Printf("   行号: %d\n", issue.Line)
			}
			fmt.Printf("   描述: %s\n", issue.Description)
			fmt.Printf("   理由: %s\n", issue.Rationale)
			fmt.Printf("   建议: %s\n", issue.Suggestion)
			if len(issue.Evidence) > 0 {
				fmt.Printf("   证据: %v\n", issue.Evidence)
			}
			fmt.Println()
		}
	} else {
		fmt.Println("✓ 未发现架构问题")
		fmt.Println()
	}

	// Print health score
	if report.Summary != nil {
		fmt.Printf("✓ 架构健康度: %d/100\n", report.Summary.HealthScore)

		// Print category scores
		for category, score := range report.Summary.CategoryScores {
			emoji := "✓"
			if score < 70 {
				emoji = "⚠️"
			}
			fmt.Printf("  %s %s: %d/100\n", emoji, category, score)
		}
		fmt.Println()
	}
}
