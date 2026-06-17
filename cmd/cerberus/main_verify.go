package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

// verifyCmd runs regression verification against known project model
func verifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify against known project model (regression mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer func() { _ = logger.Sync() }()

			projCfg := loadProjectConfig(configFlag, urlFlag, goalFlag, logger)
			projCfg = project.ResolveCredentials(projCfg)

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

			model := projCfg.Settings.AIBudget.Model
			if model == "" {
				model = cfg.LLMModel
			}
			apiKey := cfg.LLMAPIKey
			baseURL := projCfg.Settings.AIBudget.BaseURL
			if baseURL == "" {
				baseURL = cfg.LLMBaseURL
			}

			client, err := llm.NewClientWithConfig(llm.ClientConfig{
				Model:    model,
				APIKey:   apiKey,
				BaseURL:  baseURL,
				Provider: cfg.LLMProvider,
			})
			if err != nil {
				return fmt.Errorf("create LLM client: %w", err)
			}

			sess, err := session.NewSession(ctx, session.SessionConfig{
				Mode:       session.ModeVerify,
				Goal:       goalFlag,
				Config:     projCfg,
				Store:      s,
				Client:     client,
				Logger:     logger,
				Gate:       nil,
				ProjectDir: dirFlag,
			})
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}

			sess.SetupHeadDrivers(apiKey, baseURL, cfg.TierModels)

			if err := sess.Run(ctx); err != nil {
				return fmt.Errorf("session verify: %w", err)
			}

			sess.Close()
			return nil
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "Target URL (optional for local-only testing)")
	cmd.Flags().StringVar(&goalFlag, "goal", "", "Test goal description (required)")
	cmd.Flags().StringVar(&configFlag, "config", ".cerberus/project.yaml", "Project config file")
	cmd.Flags().StringVar(&dirFlag, "dir", ".", "Project root directory for file/process executors")
	_ = cmd.MarkFlagRequired("goal")
	return cmd
}
