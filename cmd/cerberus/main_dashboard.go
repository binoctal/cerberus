package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/dashboard"
	"github.com/binoctal/cerberus/internal/store"
)

// dashboardCmd runs the interactive TUI dashboard
func dashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Interactive TUI dashboard for monitoring sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()

			dbPath := cfg.DBPath
			if dbFlag != "" {
				dbPath = dbFlag
			}

			s, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = s.Close() }()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			return dashboard.Run(s)
		},
	}
	return cmd
}
