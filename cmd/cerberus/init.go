package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/store"
)

// initCmd returns the project initialization command
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
  # mode: "" (auto: infer from services), "local" (local codebase only), or "saas" (live HTTP)
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
    # token: <static WS token — for actors with no auth flow (API key / dev backdoor)>
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

			// Seed default L3 strategies into the store
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

			// Configure MCP server in .claude/settings.json
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

				// Ensure mcpServers.cerberus exists (idempotent)
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

			if hint := proxyHint(); hint != "" {
				fmt.Println(hint)
				fmt.Println()
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

// proxyHint returns guidance when a non-official Anthropic endpoint is set via
// ANTHROPIC_BASE_URL — a hardcoded ai_budget.model may not be valid there.
// Empty when no guidance is needed (no endpoint or official api.anthropic.com).
func proxyHint() string {
	base := os.Getenv("ANTHROPIC_BASE_URL")
	if base == "" || strings.Contains(base, "api.anthropic.com") {
		return ""
	}
	return fmt.Sprintf("Note: custom LLM endpoint detected (%s).\n"+
		"  If tests fail on an unknown model, set settings.ai_budget.model to \"\" in\n"+
		"  .cerberus/project.yaml to follow the environment's default model.", base)
}
