package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/dashboard"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/mcp"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/report"
	"github.com/binoctal/cerberus/internal/server"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

var (
	urlFlag            string
	goalFlag           string
	actorFlags         []string
	dbFlag             string
	configFlag         string
	portFlag           string
	deepPlanFlag       bool
	dirFlag            string
	sessionFlag        string
	formatFlag         string
	outputFlag         string
	parallelFlag       bool
	workersFlag        int
	resumeFlag         string
	autoTestSafetyFlag string

	// Set via -ldflags at build time.
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cerberus",
		Short: "Cerberus — Universal AI Testing Framework",
	}

	rootCmd.AddCommand(initCmd(), runCmd(), verifyCmd(), serveCmd(), mcpCmd(), reportCmd(), dashboardCmd(), architectureCmd(), regressionCmd(), accuracyCmd(), knownIssueCmd(), versionCmd())

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
					_, _ = f.WriteString(gitignoreEntry)
					_ = f.Close()
				}
			}

			fmt.Println("✓ Created .cerberus/project.yaml")
			fmt.Println("✓ Created .cerberus/credentials.yaml")
			fmt.Println("✓ Updated .gitignore")

			// Seed default L3 strategies into the store.
			// Try to load config first, fallback to default path
			cfg := config.Load()
			dbPath := cfg.DBPath
			seedDB, seedErr := store.New(dbPath)
			if seedErr == nil {
				seedCtx := context.Background()
				_ = store.RunMigrations(seedCtx, seedDB.DB(), "migrations")
				seedLogger, _ := zap.NewProduction()
				count, _ := store.SeedStrategies(seedCtx, seedDB, "", seedLogger)
				_ = seedDB.Close()
				if count > 0 {
					fmt.Printf("✓ Seeded %d default test strategies\n", count)
				}
			}

			// Configure MCP server in .claude/settings.json.
			claudeDir := ".claude"
			if mkdirErr := os.MkdirAll(claudeDir, 0755); mkdirErr == nil {
				settingsPath := claudeDir + "/settings.json"
				mcpEntry := map[string]any{
					"command": "cerberus",
					"args":    []string{"mcp"},
				}

				var settings map[string]any
				existing, readErr := os.ReadFile(settingsPath)
				if readErr == nil {
					_ = json.Unmarshal(existing, &settings)
				}
				if settings == nil {
					settings = make(map[string]any)
				}

				// Ensure mcpServers.cerberus exists (idempotent).
				ms, ok := settings["mcpServers"].(map[string]any)
				if !ok {
					ms = make(map[string]any)
					settings["mcpServers"] = ms
				}
				if _, exists := ms["cerberus"]; !exists {
					ms["cerberus"] = mcpEntry
					data, _ := json.MarshalIndent(settings, "", "  ")
					if writeErr := os.WriteFile(settingsPath, data, 0644); writeErr == nil {
						fmt.Println("✓ Configured .claude/settings.json for MCP integration")
					}
				}
			}

			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  1. Edit .cerberus/project.yaml with your project details")
			fmt.Println("  2. Set credentials in .cerberus/credentials.yaml or env vars")
			fmt.Println("  3. Run: cerberus run --goal \"test all APIs\"")
			fmt.Println("     (or: cerberus run --dir . --goal \"test my code\" for local-only testing)")
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
				Mode:       session.ModeRun,
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
			sess.DeepPlan = deepPlanFlag
			sess.Parallel = parallelFlag
			sess.MaxWorkers = workersFlag
			sess.AutoTestSafety = autoTestSafetyFlag
			sess.SetupHeadDrivers(apiKey, baseURL, cfg.TierModels)

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			go func() {
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
				<-sigCh
				logger.Info("interrupt received, shutting down...")
				cancel()
			}()

			if resumeFlag != "" {
				sess.ID = resumeFlag
				if err := sess.Resume(ctx); err != nil {
					return fmt.Errorf("session resume: %w", err)
				}
			} else {
				if err := sess.Run(ctx); err != nil {
					return fmt.Errorf("session run: %w", err)
				}
			}

			sess.Close()
			return nil
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "Target URL (optional for local-only testing)")
	cmd.Flags().StringVar(&goalFlag, "goal", "", "Test goal description (required)")
	cmd.Flags().StringSliceVar(&actorFlags, "actor", nil, "Actor names to use")
	cmd.Flags().StringVar(&dbFlag, "db", "", "Database URL (enables Checker)")
	cmd.Flags().StringVar(&configFlag, "config", ".cerberus/project.yaml", "Project config file")
	cmd.Flags().BoolVar(&deepPlanFlag, "deep-plan", false, "Enable ToT deep planning for comprehensive test generation")
	cmd.Flags().BoolVar(&parallelFlag, "parallel", false, "Execute independent test cases in parallel")
	cmd.Flags().IntVar(&workersFlag, "workers", 4, "Max parallel workers (use with --parallel)")
	cmd.Flags().StringVar(&dirFlag, "dir", ".", "Project root directory for file/process executors")
	cmd.Flags().StringVar(&resumeFlag, "resume", "", "Resume a previous session by ID")
	cmd.Flags().StringVar(&autoTestSafetyFlag, "auto-test-safety", "off", "AutoTest phase: off|approve|auto|dry-run")
	return cmd
}

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

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP API server (CI integration)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer func() { _ = logger.Sync() }()

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

			srv := server.New(s, cfg, logger)
			addr := ":" + portFlag

			// Graceful shutdown on signal.
			_, cancel := context.WithCancel(ctx)
			defer cancel()
			go func() {
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
				<-sigCh
				logger.Info("interrupt received, shutting down...")
				cancel()
			}()

			logger.Info("cerberus serve starting", zap.String("addr", addr))
			return srv.ListenAndServe(addr)
		},
	}
	cmd.Flags().StringVar(&portFlag, "port", "8090", "HTTP server port")
	return cmd
}

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server (for Claude Code integration)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer func() { _ = logger.Sync() }()

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

			srv := mcp.NewServer(s, logger)
			return srv.Serve(ctx, os.Stdin, os.Stdout)
		},
	}
}

func loadProjectConfig(configPath, url, goal string, logger *zap.Logger) *project.Config {
	var cfg *project.Config
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			loaded, err := project.LoadFromFile(configPath)
			if err != nil {
				logger.Warn("failed to load project config, using defaults", zap.Error(err))
			} else {
				cfg = loaded
			}
		}
	}
	if cfg == nil {
		d := project.DefaultConfig()
		cfg = &d
	}

	// If --url was provided and config has no services, create a synthetic one.
	if url != "" && len(cfg.Services) == 0 {
		cfg.Services = append(cfg.Services, project.Service{
			Name: "default",
			URL:  url,
		})
	}

	return cfg
}

func reportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate test report (HTML, Markdown, or JSON)",
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

			data, err := report.BuildReport(ctx, s, sessionFlag)
			if err != nil {
				return err
			}

			var output string
			switch formatFlag {
			case "html":
				html, err := report.RenderHTMLString(data)
				if err != nil {
					return fmt.Errorf("render HTML: %w", err)
				}
				output = html
				case "json":
					// If stats non-empty, merge autotest into top-level
					if data.Session.Stats != "" && data.Session.Stats != "{}" {
						var statsMap map[string]interface{}
						if err := json.Unmarshal([]byte(data.Session.Stats), &statsMap); err == nil {
							// Add autotest if present
							if data.AutoTest != nil {
								statsMap["autotest"] = data.AutoTest
							}
							b, _ := json.MarshalIndent(statsMap, "", "  ")
							output = string(b)
						} else {
							// Fallback to raw stats on error
							output = data.Session.Stats
						}
					} else {
						// Otherwise marshal full ReportData (AutoTest included via json tag)
						b, _ := json.MarshalIndent(data, "", "  ")
						output = string(b)
					}
			case "junit":
				xml, err := report.RenderJUnit(data)
				if err != nil {
					return fmt.Errorf("render JUnit: %w", err)
				}
				output = string(xml)
			case "markdown", "":
				output = report.RenderMarkdown(data)
			default:
				return fmt.Errorf("unsupported format: %s (use html, junit, markdown, or json)", formatFlag)
			}

			if outputFlag != "" {
				if err := os.WriteFile(outputFlag, []byte(output), 0644); err != nil {
					return fmt.Errorf("write file: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Report written to %s\n", outputFlag)
			} else {
				fmt.Println(output)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionFlag, "session", "", "Session ID to report")
	cmd.Flags().StringVar(&formatFlag, "format", "markdown", "Output format: html, junit, markdown, json")
	cmd.Flags().StringVar(&outputFlag, "output", "", "Output file path (default: stdout)")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

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

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("cerberus %s (commit: %s, built: %s)\n", version, commit, date)
		},
	}
}

func containsLine(content, line string) bool {
	return strings.Contains(content, line)
}
