package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	urlFlag      string
	goalFlag     string
	actorFlags   []string
	dbFlag       string
	configFlag   string
	portFlag     string
	deepPlanFlag bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cerberus",
		Short: "Cerberus — Universal AI Testing Framework",
	}

	rootCmd.AddCommand(initCmd(), runCmd(), verifyCmd(), serveCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize project configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ".cerberus"
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("create .cerberus dir: %w", err)
			}

			projectYAML := `project:
  name: ""

services:
  - name: web
    url: "http://localhost:3000"
    health: "/"

actors:
  - name: admin
    credentials:
      email: "${ADMIN_EMAIL}"
      password: "${ADMIN_PASS}"
    entry: "/admin"

databases: []

invariants: []

settings:
  max_duration: 30m
  confidence_threshold: 0.7
  auto_fix: low_only
  ai_budget:
    session_total_tokens: 200000
    per_call_limit: 10000
    model: "claude-sonnet-4-6"
`
			if err := os.WriteFile(dir+"/project.yaml", []byte(projectYAML), 0644); err != nil {
				return err
			}

			credYAML := `# Credentials — DO NOT commit this file
# Add to .gitignore
actors:
  admin:
    email: admin@example.com
    password: changeme
`
			if err := os.WriteFile(dir+"/credentials.yaml", []byte(credYAML), 0644); err != nil {
				return err
			}

		gitignoreEntry := ".cerberus/credentials.yaml\n"
		existing, _ := os.ReadFile(".gitignore")
		if !containsLine(string(existing), ".cerberus/credentials.yaml") {
			f, err := os.OpenFile(".gitignore", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				f.WriteString(gitignoreEntry)
				f.Close()
			}
		}

			fmt.Println("✓ Created .cerberus/project.yaml")
			fmt.Println("✓ Created .cerberus/credentials.yaml")
			fmt.Println("✓ Updated .gitignore")
			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  1. Edit .cerberus/project.yaml with your project details")
			fmt.Println("  2. Set credentials in .cerberus/credentials.yaml or env vars")
			fmt.Println("  3. Run: cerberus run --url http://localhost:3000 --goal \"test all APIs\"")
			return nil
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "Target URL")
	return cmd
}

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run intelligent tests (cognition + exploration + judgment)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer logger.Sync()

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
			defer s.Close()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			model := projCfg.Settings.AIBudget.Model
			if model == "" {
				model = cfg.LLMModel
			}
			apiKey := cfg.LLMAPIKey

			client, err := llm.NewClient(model, apiKey)
			if err != nil {
				return fmt.Errorf("create LLM client: %w", err)
			}

			sess, err := session.NewSession(ctx, session.ModeRun, goalFlag, projCfg, s, client, logger)
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}
			sess.DeepPlan = deepPlanFlag

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			go func() {
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
				<-sigCh
				logger.Info("interrupt received, shutting down...")
				cancel()
			}()

			if err := sess.Run(ctx); err != nil {
				return fmt.Errorf("session run: %w", err)
			}

			sess.Close()
			return nil
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "Target URL (required)")
	cmd.Flags().StringVar(&goalFlag, "goal", "", "Test goal description (required)")
	cmd.Flags().StringSliceVar(&actorFlags, "actor", nil, "Actor names to use")
	cmd.Flags().StringVar(&dbFlag, "db", "", "Database URL (enables Checker)")
	cmd.Flags().StringVar(&configFlag, "config", ".cerberus/project.yaml", "Project config file")
	cmd.Flags().BoolVar(&deepPlanFlag, "deep-plan", false, "Enable ToT deep planning for comprehensive test generation")
	_ = cmd.MarkFlagRequired("url")
	_ = cmd.MarkFlagRequired("goal")
	return cmd
}

func verifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify against known project model (regression mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer logger.Sync()

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
			defer s.Close()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			model := projCfg.Settings.AIBudget.Model
			if model == "" {
				model = cfg.LLMModel
			}
			apiKey := cfg.LLMAPIKey

			client, err := llm.NewClient(model, apiKey)
			if err != nil {
				return fmt.Errorf("create LLM client: %w", err)
			}

			sess, err := session.NewSession(ctx, session.ModeVerify, goalFlag, projCfg, s, client, logger)
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}

			if err := sess.Run(ctx); err != nil {
				return fmt.Errorf("session verify: %w", err)
			}

			sess.Close()
			return nil
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "Target URL (required)")
	cmd.Flags().StringVar(&goalFlag, "goal", "", "Test goal description (required)")
	cmd.Flags().StringVar(&configFlag, "config", ".cerberus/project.yaml", "Project config file")
	_ = cmd.MarkFlagRequired("url")
	_ = cmd.MarkFlagRequired("goal")
	return cmd
}

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP API server (CI integration)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("serve mode: not yet implemented (requires C5 + C6)")
			return nil
		},
	}
	cmd.Flags().StringVar(&portFlag, "port", "8090", "HTTP server port")
	return cmd
}

func loadProjectConfig(configPath, url, goal string, logger *zap.Logger) *project.Config {
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			cfg, err := project.LoadFromFile(configPath)
			if err != nil {
				logger.Warn("failed to load project config, using defaults", zap.Error(err))
				d := project.DefaultConfig()
				return &d
			}
			return cfg
		}
	}
	d := project.DefaultConfig()
	return &d
}

func containsLine(content, line string) bool {
	return strings.Contains(content, line)
}
