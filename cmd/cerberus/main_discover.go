package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/discover"
	"github.com/binoctal/cerberus/internal/project"
)

var (
	discoverComposePath string
	discoverDryRun      bool
	discoverInclude     []string
	discoverExclude     []string
)

func discoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover services from docker-compose.yml into project.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscover(".", discoverComposePath, discoverInclude, discoverExclude, discoverDryRun)
		},
	}
	cmd.Flags().StringVar(&discoverComposePath, "compose", "docker-compose.yml", "docker-compose file to read")
	cmd.Flags().BoolVar(&discoverDryRun, "dry-run", false, "print result without writing project.yaml")
	cmd.Flags().StringSliceVar(&discoverInclude, "include", nil, "service names to force-include")
	cmd.Flags().StringSliceVar(&discoverExclude, "exclude", nil, "service names to force-exclude")
	return cmd
}

func runDiscover(workDir, composePath string, include, exclude []string, dryRun bool) error {
	data, err := os.ReadFile(filepath.Join(workDir, composePath))
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}
	parsed, err := discover.ParseCompose(data)
	if err != nil {
		return fmt.Errorf("parse compose: %w", err)
	}
	filtered := discover.FilterServices(parsed.Services, include, exclude)
	if len(filtered) == 0 {
		return fmt.Errorf("no discoverable services (all filtered as infra or portless; use --include to force)")
	}
	services := discover.ToProjectServices(filtered)

	cfg := &project.Config{}
	cfgPath := filepath.Join(workDir, ".cerberus", "project.yaml")
	if existing, err := project.LoadFromFile(cfgPath); err == nil {
		cfg = existing
	}
	added := discover.MergeIntoConfig(cfg, services)
	hasActorKey := len(cfg.Actors) > 0

	fmt.Printf("discovered %d service(s); added %d new: %v\n", len(filtered), len(added), added)
	fmt.Print(discover.FormatGaps(discover.Gaps(cfg.Services), hasActorKey))

	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(workDir, ".cerberus"), 0755); err != nil {
		return err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, out, 0644)
}
