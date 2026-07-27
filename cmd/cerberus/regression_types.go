package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/logging"
	"github.com/binoctal/cerberus/internal/store"
)

var (
	regressionCategoryFlag string
	regressionVerboseFlag  bool
	accuracyLimitFlag      int
)

func regressionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "regression",
		Short: "Run regression tests for architecture analyzer",
		Long:  "运行架构分析器的回归测试，确保已修复的bug不会重现",
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

			return runRegressionTests(ctx, s, logger, regressionCategoryFlag, regressionVerboseFlag)
		},
	}

	cmd.Flags().StringVarP(&regressionCategoryFlag, "category", "c", "", "测试类别 (complexity/abstraction/solid)")
	cmd.Flags().BoolVarP(&regressionVerboseFlag, "verbose", "v", false, "详细输出")

	return cmd
}
