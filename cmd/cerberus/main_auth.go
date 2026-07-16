package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/authdiscover"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

var (
	authDiscoverActor  string
	authDiscoverSvc    string
	authDiscoverDryRun bool
)

// authCmd is the parent for auth-related subcommands.
func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authoring aids for declarative auth flows",
	}
	cmd.AddCommand(authDiscoverCmd())
	return cmd
}

func authDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Infer an auth flow from source and write it to project.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			driver, err := newAuthDiscoverDriver()
			if err != nil {
				return err
			}
			return runAuthDiscover(cmd.Context(), ".", driver, authDiscoverOpts{
				Actor:   authDiscoverActor,
				Service: authDiscoverSvc,
				DryRun:  authDiscoverDryRun,
				confirm: promptConfirm(os.Stdin, os.Stdout),
			})
		},
	}
	cmd.Flags().StringVar(&authDiscoverActor, "actor", "", "actor whose auth: block is written (required)")
	cmd.Flags().StringVar(&authDiscoverSvc, "service", "", "service whose source is read (default: actor.Service, else first)")
	cmd.Flags().BoolVar(&authDiscoverDryRun, "dry-run", false, "print suggestion without writing")
	_ = cmd.MarkFlagRequired("actor")
	return cmd
}

// authDiscoverOpts holds parsed command inputs. confirm abstracts the y/N
// prompt so tests inject a deterministic answerer.
type authDiscoverOpts struct {
	Actor   string
	Service string
	DryRun  bool
	confirm func(prompt string) bool
}

// runAuthDiscover is the testable core. It loads project.yaml, infers the
// AuthFlow via authdiscover, prints it, and on confirmation writes it back
// (whole-file rewrite). ErrNoAuthFlow is reported via stdout, not returned.
func runAuthDiscover(ctx context.Context, workDir string, driver *ai.Driver, opts authDiscoverOpts) error {
	if opts.Actor == "" {
		return errors.New("--actor is required")
	}
	cfgPath := filepath.Join(workDir, ".cerberus", "project.yaml")
	cfg, err := project.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("load project.yaml: %w", err)
	}
	serviceURL := resolveServiceURL(cfg, opts.Actor, opts.Service)

	af, err := authdiscover.Discover(ctx, driver, cfg, opts.Actor, serviceURL)
	if errors.Is(err, authdiscover.ErrNoAuthFlow) {
		fmt.Printf("no login endpoint found for actor %q\n", opts.Actor)
		return nil
	}
	if err != nil {
		return err
	}

	// Render only the auth block (no credential values live here — only
	// placeholders and endpoint shape).
	block, _ := yaml.Marshal(map[string]any{"auth": af})
	fmt.Printf("Suggested auth for %q:\n%s\n", opts.Actor, string(block))

	if opts.DryRun {
		return nil
	}

	existing := actorAuthPath(cfg, opts.Actor) != ""
	question := fmt.Sprintf("Write to actor %q in project.yaml? [y/N]", opts.Actor)
	if existing {
		question = fmt.Sprintf("Actor %q already has an auth block. Overwrite? [y/N]", opts.Actor)
	}
	if opts.confirm == nil || !opts.confirm(question) {
		fmt.Println("aborted; no changes written")
		return nil
	}

	setActorAuth(cfg, opts.Actor, af)
	if err := os.MkdirAll(filepath.Join(workDir, ".cerberus"), 0755); err != nil {
		return err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, out, 0644)
}

// resolveServiceURL mirrors Session.serviceURLForActor (Component 2): the
// actor's service if set, else the named service, else the first service.
func resolveServiceURL(cfg *project.Config, actorName, serviceFlag string) string {
	for i := range cfg.Actors {
		if cfg.Actors[i].Name == actorName {
			name := serviceFlag
			if name == "" {
				name = cfg.Actors[i].Service
			}
			for _, svc := range cfg.Services {
				if svc.Name == name {
					return svc.URL
				}
			}
			break
		}
	}
	if len(cfg.Services) > 0 {
		return cfg.Services[0].URL
	}
	return ""
}

func actorAuthPath(cfg *project.Config, actorName string) string {
	for _, a := range cfg.Actors {
		if a.Name == actorName && a.Auth != nil {
			return a.Auth.Login.Path
		}
	}
	return ""
}

func setActorAuth(cfg *project.Config, actorName string, af *project.AuthFlow) {
	for i := range cfg.Actors {
		if cfg.Actors[i].Name == actorName {
			cfg.Actors[i].Auth = af
			return
		}
	}
}

// newAuthDiscoverDriver builds the single LLM driver from global config + the
// project's model. No LLM-client code is hidden inside authdiscover.
func newAuthDiscoverDriver() (*ai.Driver, error) {
	gcfg := config.Load()
	projCfg, err := project.LoadFromFile(filepath.Join(".", ".cerberus", "project.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load project.yaml: %w", err)
	}
	model := projCfg.Settings.AIBudget.Model
	client, err := llm.NewClientWithConfig(llm.ClientConfig{
		Model:      model,
		APIKey:     gcfg.LLMAPIKey,
		BaseURL:    gcfg.LLMBaseURL,
		AuthScheme: gcfg.LLMAuthScheme,
	})
	if err != nil {
		return nil, fmt.Errorf("build llm client: %w", err)
	}
	total := projCfg.Settings.AIBudget.SessionTotalTokens
	if total <= 0 {
		total = 200000
	}
	perCall := projCfg.Settings.AIBudget.PerCallLimit
	if perCall <= 0 {
		perCall = 10000
	}
	return ai.NewDriver(client, ai.NewTokenBudget(total, perCall)), nil
}

// promptConfirm returns a confirmer that reads a y/N line from in.
func promptConfirm(in io.Reader, out io.Writer) func(string) bool {
	return func(question string) bool {
		if _, err := fmt.Fprint(out, question+" "); err != nil {
			return false
		}
		scanner := bufio.NewScanner(in)
		if !scanner.Scan() {
			return false
		}
		line := strings.ToLower(strings.TrimSpace(scanner.Text()))
		return line == "y" || line == "yes"
	}
}
