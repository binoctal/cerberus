package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/logging"
	"github.com/binoctal/cerberus/internal/store"
)

func accuracyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accuracy",
		Short: "Show accuracy reports and history",
		Long:  "显示架构分析器的准确率报告和历史数据",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger := logging.NewLogger(cfg.LogLevel, cfg.Paths.LogsDir)
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
