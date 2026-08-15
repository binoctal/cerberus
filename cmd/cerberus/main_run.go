package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/discover"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/logging"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

// runCmd executes the main test run command
func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run intelligent tests (cognition + exploration + judgment)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger := logging.NewLogger(cfg.LogLevel, cfg.Paths.LogsDir)
			defer func() { _ = logger.Sync() }()

			projCfg := loadProjectConfig(configFlag, urlFlag, goalFlag, logger)
			projCfg = project.ResolveCredentials(projCfg)

			// Hint to run discover if services look unconfigured
			if fileExists("docker-compose.yml") {
				if discover.ShouldHintDiscover(projCfg.Services, true) {
					fmt.Println(discover.HintMessage)
				}
			}

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
				Model:      model,
				APIKey:     apiKey,
				BaseURL:    baseURL,
				Provider:   cfg.LLMProvider,
				AuthScheme: cfg.LLMAuthScheme,
			})
			if err != nil {
				return fmt.Errorf("create LLM client: %w", err)
			}

			sessCfg := session.SessionConfig{
				Mode:       session.ModeRun,
				Goal:       goalFlag,
				Config:     projCfg,
				Store:      s,
				Client:     client,
				Logger:     logger,
				Gate:       nil,
				ProjectDir: dirFlag,
			}
			var sess *session.Session
			if resumeFlag != "" {
				sess, err = session.NewSessionForResume(ctx, sessCfg, resumeFlag)
			} else {
				sess, err = session.NewSession(ctx, sessCfg)
			}
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}
			sess.DeepPlan = deepPlanFlag
			sess.Parallel = parallelFlag
			sess.MaxWorkers = workersFlag
			sess.AutoTestSafety = autoTestSafetyFlag
			sess.SetupHeadDrivers(apiKey, baseURL, cfg.LLMAuthScheme, cfg.TierModels)

			// Auto-extract the claims ledger before the run so the gate has a
			// denominator even on projects that never ran `cerberus claims
			// extract`. Silent no-op when a ledger exists or no doc source is
			// found; an extraction failure skips (logged) rather than failing
			// the run — the gate only bites on a ledger that exists.
			claimsTotal, claimsPerCall := projCfg.Settings.AIBudget.SessionTotalTokens, projCfg.Settings.AIBudget.PerCallLimit
			if claimsTotal <= 0 {
				claimsTotal = 200000
			}
			if claimsPerCall <= 0 {
				claimsPerCall = 10000
			}
			claimsDrv := ai.NewDriver(client, ai.NewTokenBudget(claimsTotal, claimsPerCall))
			if _, err := autoExtractClaims(ctx, dirFlag, projCfg, claimsDrv, func(msg string) { logger.Info(msg) }); err != nil {
				logger.Warn(fmt.Sprintf("claims auto-extract skipped: %v", err))
			}

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			go func() {
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
				<-sigCh
				logger.Info("interrupt received, shutting down...")
				cancel()
			}()

			var runErr error
			if resumeFlag != "" {
				runErr = sess.Resume(ctx)
			} else {
				runErr = sess.Run(ctx)
			}
			if runErr != nil {
				if code := mapRunExitError(runErr); code != 0 {
					// Sentinel-mapped failures (claims gate) already logged the
					// summary via finalize; exit with their dedicated code.
					// os.Exit skips the deferred closes, so run them first.
					sess.Close()
					_ = s.Close()
					_ = logger.Sync()
					os.Exit(code)
				}
				if resumeFlag != "" {
					return fmt.Errorf("session resume: %w", runErr)
				}
				return fmt.Errorf("session run: %w", runErr)
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

// mapRunExitError maps a session run/resume error to a dedicated process exit
// code. The claims gate gets 3 — a session that is incomplete (a critical
// claim unproven) is distinct from execution failure (1, cobra's default for
// the returned error). 0 means "no dedicated code": return the error and let
// the generic path handle it.
func mapRunExitError(err error) int {
	if errors.Is(err, session.ErrClaimsGate) {
		return 3
	}
	return 0
}

// fileExists checks if a file exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
