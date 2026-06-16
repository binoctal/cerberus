package main

import (
	"fmt"
	"os"

	"github.com/binoctal/cerberus/internal/architecture"
)

func main() {
	projectPath := "."
	if len(os.Args) > 1 {
		projectPath = os.Args[1]
	}

	analyzer := architecture.NewAnalyzer(projectPath)
	report, err := analyzer.Analyze()
	if err != nil {
		fmt.Printf("分析失败: %v\n", err)
		os.Exit(1)
	}

	// 打印报告
	fmt.Println("🏗️  架构质量检查报告")
	fmt.Println()
	fmt.Printf("📊 代码度量:\n")
	fmt.Printf("  总文件数: %d\n", report.Metrics.TotalFiles)
	fmt.Printf("  总代码行数: %d\n", report.Metrics.TotalLines)
	if report.Metrics.TotalFiles > 0 {
		fmt.Printf("  平均行数/文件: %d\n", report.Metrics.TotalLines/report.Metrics.TotalFiles)
	}
	fmt.Printf("  最大行数/文件: %d\n", report.Metrics.MaxLinesPerFile)
	fmt.Println()

	fmt.Printf("⚠️  发现 %d 个架构问题:\n\n", len(report.Issues))

	for i, issue := range report.Issues {
		fmt.Printf("%d. [%s] %s (%s)\n", i+1, issue.Severity, issue.Type, issue.File)
		fmt.Printf("   %s\n", issue.Description)
		if issue.Rationale != "" {
			fmt.Printf("   理由: %s\n", issue.Rationale)
		}
		if issue.Suggestion != "" {
			fmt.Printf("   建议: %s\n", issue.Suggestion)
		}
		fmt.Println()
	}

	fmt.Printf("✓ 架构健康度: %d/100\n", report.Summary.HealthScore)
	
	for category, score := range report.Summary.CategoryScores {
		status := "✓"
		if score < 70 {
			status = "⚠️"
		}
		fmt.Printf("  %s %s: %d/100\n", status, category, score)
	}
}
