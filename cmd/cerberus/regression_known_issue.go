package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/logging"
	"github.com/binoctal/cerberus/internal/store"
)

func knownIssueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "known-issue",
		Short: "Manage known issues and false positives",
		Long:  "管理已知问题和误报记录",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List known issues",
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

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a known issue",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("添加已知问题 - 功能开发中")
			return nil
		},
	}

	cmd.AddCommand(listCmd, addCmd)
	return cmd
}
