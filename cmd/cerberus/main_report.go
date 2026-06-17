package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/report"
	"github.com/binoctal/cerberus/internal/store"
)

// reportCmd generates test reports in various formats
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
