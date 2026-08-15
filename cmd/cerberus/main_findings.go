package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

// findingsDB is the --db override shared by the findings subcommands.
var findingsDB string

func findingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "findings",
		Short: "Observed-defect ledger (.cerberus/findings.yaml)",
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "Render the findings ledger",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFindingsList(os.Stdout)
		},
	}
	pull := &cobra.Command{
		Use:   "pull",
		Short: "Backfill findings from a persisted session (default: the latest)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath := findingsDB
			if dbPath == "" {
				dbPath = config.Load().DBPath
			}
			return runFindingsPull(cmd.Context(), ".", dbPath, findingsSession, os.Stdout)
		},
	}
	pull.Flags().StringVar(&findingsSession, "session", "", "session id (default: the latest in the store)")
	pull.Flags().StringVar(&findingsDB, "db", "", "database path (default: the configured cerberus DB)")
	cmd.AddCommand(list, pull)
	return cmd
}

var findingsSession string

func runFindingsList(w io.Writer) error {
	ff, err := project.LoadFindings(".")
	if err != nil {
		return err
	}
	if ff == nil {
		_, err := fmt.Fprintln(w, "no findings ledger (.cerberus/findings.yaml)")
		return err
	}
	if len(ff.Findings) == 0 {
		_, err := fmt.Fprintln(w, "findings: (none)")
		return err
	}
	for _, f := range ff.Findings {
		fmt.Fprintf(w, "%s [%s/%s] x%d — %s\n", f.ID, f.Status, f.Tier, f.Count, f.Summary)
		if len(f.ClaimRefs) > 0 {
			fmt.Fprintf(w, "    claims: %v\n", f.ClaimRefs)
		}
		fmt.Fprintf(w, "    case: %s (last session %s, first %s)\n", f.CaseRef, f.SessionRef, f.FirstSeen)
	}
	return nil
}

// runFindingsPull backfills findings from a persisted session: the plan
// supplies the cases, the verdicts the outcomes (same semantics as the
// finalize-time backflow — a target with verdicts but no passing one).
func runFindingsPull(ctx context.Context, workDir, dbPath, sessionID string, w io.Writer) error {
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("findings pull: database %s: %w", dbPath, err)
	}
	s, err := store.New(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	if sessionID == "" {
		sessions, err := s.ListSessions(ctx, 1)
		if err != nil || len(sessions) == 0 {
			return fmt.Errorf("findings pull: no sessions in the store")
		}
		sessionID = sessions[0].ID
	}
	var plan agent.TestPlan
	if err := s.LoadPlan(ctx, sessionID, &plan); err != nil {
		return fmt.Errorf("findings pull: session %s has no plan: %w", sessionID, err)
	}
	verdicts, err := s.GetVerdicts(ctx, sessionID)
	if err != nil {
		return err
	}
	cfg, err := project.LoadFromFile(filepath.Join(workDir, ".cerberus", "project.yaml"))
	if err != nil {
		return err
	}
	before := 0
	if ff, _ := project.LoadFindings(workDir); ff != nil {
		before = len(ff.Findings)
	}
	if err := session.PullFindings(workDir, cfg, &plan, verdicts, sessionID, zap.NewNop()); err != nil {
		return err
	}
	after := before
	if ff, _ := project.LoadFindings(workDir); ff != nil {
		after = len(ff.Findings)
	}
	fmt.Fprintf(w, "session %s: findings %d -> %d\n", sessionID, before, after)
	return nil
}
