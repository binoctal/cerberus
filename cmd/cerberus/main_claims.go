package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/claimsdiscover"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

var (
	claimsExtractFrom  string
	claimsExtractMax   int
	claimsExtractPrune bool
	claimsCheckDB      string
)

// claimsCmd is the parent for claims-ledger subcommands.
func claimsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claims",
		Short: "Claims ledger: extract from docs, list, and check against the store",
	}
	cmd.AddCommand(claimsExtractCmd())
	cmd.AddCommand(claimsListCmd())
	cmd.AddCommand(claimsCheckCmd())
	return cmd
}

func claimsExtractCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Extract claims from a doc source via the LLM and auto-merge into .cerberus/claims.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			driver, err := newClaimsDriver()
			if err != nil {
				return err
			}
			return runClaimsExtract(cmd.Context(), ".", driver, claimsExtractOpts{
				From:  claimsExtractFrom,
				Max:   claimsExtractMax,
				Prune: claimsExtractPrune,
			}, os.Stdout)
		},
	}
	cmd.Flags().StringVar(&claimsExtractFrom, "from", "", "path to a doc file or dir to extract from (required)")
	cmd.Flags().IntVar(&claimsExtractMax, "max", claimsdiscover.DefaultMaxClaims, "max claims to extract")
	cmd.Flags().BoolVar(&claimsExtractPrune, "prune", false, "drop ledger ids absent from the draft (default append-only)")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

func claimsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Render the claims ledger",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClaimsList(".", os.Stdout)
		},
	}
}

func claimsCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Print per-claim verdicts from the latest session in the store",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath := claimsCheckDB
			if dbPath == "" {
				dbPath = config.Load().DBPath
			}
			return runClaimsCheck(cmd.Context(), ".", dbPath, os.Stdout)
		},
	}
	cmd.Flags().StringVar(&claimsCheckDB, "db", "", "database path (default: the configured cerberus DB)")
	return cmd
}

// claimsExtractOpts holds the extract command inputs.
type claimsExtractOpts struct {
	From  string
	Max   int
	Prune bool
}

// claimsSource is one doc fed to extraction: Display is the path recorded in
// the ledger, Abs the on-disk path for hashing.
type claimsSource struct {
	Display string
	Abs     string
	Content string
}

// loadClaimsSources reads --from (file or dir of text files, mirroring the
// protocol infer input rules) into ordered sources.
func loadClaimsSources(workDir, from string) ([]claimsSource, error) {
	path := filepath.Join(workDir, from)
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []claimsSource{{Display: from, Abs: path, Content: string(data)}}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var out []claimsSource
	for _, e := range entries {
		if e.IsDir() || !isTextFile(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(path, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, claimsSource{
			Display: filepath.Join(from, e.Name()),
			Abs:     filepath.Join(path, e.Name()),
			Content: string(data),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no readable text files under %s", from)
	}
	return out, nil
}

// claimsSourceFile aliases the ledger's anonymous source-file entry so
// hashSources can build the slice without restating the struct.
type claimsSourceFile = struct {
	Path string `yaml:"path"`
	Hash string `yaml:"hash,omitempty"`
}

// hashSources records each source file with its SHA-256 hash (vocab
// convention: the hash is the change detector for re-extraction).
func hashSources(sources []claimsSource) ([]claimsSourceFile, error) {
	files := make([]claimsSourceFile, 0, len(sources))
	for _, s := range sources {
		data, err := os.ReadFile(s.Abs)
		if err != nil {
			return nil, fmt.Errorf("hash source: %w", err)
		}
		sum := sha256.Sum256(data)
		files = append(files, claimsSourceFile{
			Path: s.Display,
			Hash: "sha256:" + hex.EncodeToString(sum[:]),
		})
	}
	return files, nil
}

// runClaimsExtract is the testable core: load docs, Extract via the LLM,
// SurfaceTriage against project.yaml's declared surfaces, MergeClaims into
// the existing ledger (append-only unless --prune), and write
// .cerberus/claims.yaml. A broken existing ledger is an error, never
// clobbered.
func runClaimsExtract(ctx context.Context, workDir string, driver *ai.Driver, opts claimsExtractOpts, w io.Writer) error {
	cfg, err := project.LoadFromFile(filepath.Join(workDir, ".cerberus", "project.yaml"))
	if err != nil {
		return fmt.Errorf("load project.yaml: %w", err)
	}
	sources, err := loadClaimsSources(workDir, opts.From)
	if err != nil {
		return fmt.Errorf("read --from: %w", err)
	}
	docs := make(map[string]string, len(sources))
	for _, s := range sources {
		docs[s.Display] = s.Content
	}
	draft, err := claimsdiscover.Extract(ctx, driver, docs, opts.Max)
	if err != nil {
		if errors.Is(err, claimsdiscover.ErrNoClaims) {
			_, err := fmt.Fprintln(w, "no claims extracted")
			return err
		}
		return err
	}
	triaged := claimsdiscover.SurfaceTriage(draft, cfg)
	existing, err := project.LoadClaims(workDir)
	if err != nil {
		return fmt.Errorf("existing ledger: %w", err)
	}
	merged := claimsdiscover.MergeClaims(existing, triaged, opts.Prune)
	files, err := hashSources(sources)
	if err != nil {
		return err
	}
	merged.Source.Files = files
	if err := writeClaimsFile(workDir, merged); err != nil {
		return err
	}
	_, err = fmt.Fprint(w, renderClaimsLedger(merged))
	return err
}

// writeClaimsFile validates and writes the ledger document.
func writeClaimsFile(workDir string, cf *project.ClaimsFile) error {
	if err := project.ValidateClaims(cf); err != nil {
		return fmt.Errorf("claims.yaml: %w", err)
	}
	block, err := yaml.Marshal(cf)
	if err != nil {
		return err
	}
	outPath := filepath.Join(workDir, ".cerberus", "claims.yaml")
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outPath, block, 0644)
}

// renderClaimsLedger renders the ledger header plus one line per claim.
func renderClaimsLedger(cf *project.ClaimsFile) string {
	critical := 0
	for _, c := range cf.Claims {
		if c.Critical {
			critical++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "claims ledger (%d claims, %d critical):\n", len(cf.Claims), critical)
	for _, c := range cf.Claims {
		line := "  " + c.ID
		if c.Critical {
			line += " [critical]"
		}
		if c.StatusAnnotation != "" {
			line += " (" + c.StatusAnnotation + ")"
		}
		line += " — " + c.Text
		if c.SourceRef != "" {
			line += " [" + c.SourceRef + "]"
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

// runClaimsList renders the ledger; a missing ledger is reported, not an
// error (the ledger is optional per project).
func runClaimsList(workDir string, w io.Writer) error {
	cf, err := project.LoadClaims(workDir)
	if err != nil {
		return err
	}
	if cf == nil {
		_, err := fmt.Fprintln(w, "no claims ledger (.cerberus/claims.yaml)")
		return err
	}
	_, err = fmt.Fprint(w, renderClaimsLedger(cf))
	return err
}

// runClaimsCheck prints per-claim verdicts from the latest session in the
// store. It reads the persisted summary verdicts rather than re-running
// session.ReconcileClaims: reconciliation needs the session's in-memory step
// results, which are not reconstructible from the store. With no store or no
// sessions, it renders the ledger with no verdicts.
func runClaimsCheck(ctx context.Context, workDir, dbPath string, w io.Writer) error {
	cf, err := project.LoadClaims(workDir)
	if err != nil {
		return err
	}
	if cf == nil {
		_, err := fmt.Fprintln(w, "no claims ledger (.cerberus/claims.yaml)")
		return err
	}
	verdicts := latestClaimsVerdicts(ctx, dbPath)
	if len(verdicts) == 0 {
		_, err := fmt.Fprint(w, renderClaimsLedger(cf)+
			"  (no verdicts — no reconciled session in the store)\n")
		return err
	}
	byID := make(map[string]session.ClaimVerdict, len(verdicts))
	for _, v := range verdicts {
		byID[v.Claim.ID] = v
	}
	var b strings.Builder
	for _, c := range cf.Claims {
		if v, ok := byID[c.ID]; ok {
			fmt.Fprintf(&b, "%s: %s", c.ID, v.Status)
			if len(v.Cases) > 0 {
				fmt.Fprintf(&b, " (cases: %s)", strings.Join(v.Cases, ", "))
			}
			fmt.Fprintln(&b)
			continue
		}
		fmt.Fprintf(&b, "%s: no verdict (not in the latest session)\n", c.ID)
	}
	_, err = fmt.Fprint(w, b.String())
	return err
}

// latestClaimsVerdicts loads the newest session's persisted claims verdicts
// from the store. Any store problem (missing DB, unmigrated schema, no
// sessions) yields nil — check degrades to the ledger-only rendering. It does
// not run migrations: `cerberus run` owns schema creation, and a check against
// a fresh/unmigrated DB simply has no verdicts to show.
func latestClaimsVerdicts(ctx context.Context, dbPath string) []session.ClaimVerdict {
	if dbPath == "" {
		return nil
	}
	// Do not materialize a DB just to check it: no file, no verdicts.
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	s, err := store.New(dbPath)
	if err != nil {
		return nil
	}
	defer func() { _ = s.Close() }()
	sessions, err := s.ListSessions(ctx, 1)
	if err != nil || len(sessions) == 0 {
		return nil
	}
	var summary session.SessionSummary
	if err := json.Unmarshal([]byte(sessions[0].Stats), &summary); err != nil {
		return nil
	}
	return summary.ClaimsVerdicts
}

// autoExtractClaims backs `cerberus run`'s pre-run extraction: when no
// claims.yaml exists and a plausible doc source is found (README* under the
// project dir, else the SUT repo root located like vocab source paths —
// ../README.md relative to the first process workdir), it runs
// Extract+Triage+Merge silently and reports one log line. ok is false when
// there was nothing to do (ledger present or no doc source).
func autoExtractClaims(ctx context.Context, workDir string, cfg *project.Config, drv *ai.Driver, log func(string)) (bool, error) {
	if _, err := os.Stat(filepath.Join(workDir, ".cerberus", "claims.yaml")); err == nil {
		return false, nil
	}
	src := findClaimsDocSource(workDir, cfg)
	if src == nil {
		return false, nil
	}
	draft, err := claimsdiscover.Extract(ctx, drv, map[string]string{src.Display: src.Content}, claimsdiscover.DefaultMaxClaims)
	if err != nil {
		if errors.Is(err, claimsdiscover.ErrNoClaims) {
			return false, nil
		}
		return false, err
	}
	merged := claimsdiscover.MergeClaims(nil, claimsdiscover.SurfaceTriage(draft, cfg), false)
	files, err := hashSources([]claimsSource{*src})
	if err != nil {
		return false, err
	}
	merged.Source.Files = files
	if err := writeClaimsFile(workDir, merged); err != nil {
		return false, err
	}
	critical := 0
	for _, c := range merged.Claims {
		if c.Critical {
			critical++
		}
	}
	log(fmt.Sprintf("claims ledger extracted (%d claims, %d critical)", len(merged.Claims), critical))
	return true, nil
}

// findClaimsDocSource probes for a doc source: README* directly under the
// project dir, else README* one level above the first actor's process workdir
// (the dogfood convention: workdir points into the SUT repo, so the repo
// root README sits at ../README.md). Returns nil when nothing plausible
// exists — auto-extract then stays a no-op.
func findClaimsDocSource(workDir string, cfg *project.Config) *claimsSource {
	if src := firstReadme(workDir, workDir); src != nil {
		return src
	}
	if cfg == nil {
		return nil
	}
	for _, a := range cfg.Actors {
		if a.Process == nil || a.Process.Workdir == "" || strings.Contains(a.Process.Workdir, "{{") {
			continue
		}
		wd := a.Process.Workdir
		if !filepath.IsAbs(wd) {
			wd = filepath.Join(workDir, wd)
		}
		if src := firstReadme(workDir, filepath.Dir(wd)); src != nil {
			return src
		}
		break // first process workdir only, mirroring "first service" intent
	}
	return nil
}

// firstReadme returns the first README* file under dir, with Display relative
// to workDir when possible (else absolute).
func firstReadme(workDir, dir string) *claimsSource {
	matches, err := filepath.Glob(filepath.Join(dir, "README*"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	abs := matches[0]
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	display := abs
	if rel, err := filepath.Rel(workDir, abs); err == nil && !strings.HasPrefix(rel, "..") {
		display = rel
	}
	return &claimsSource{Display: display, Abs: abs, Content: string(data)}
}

// newClaimsDriver builds the extraction LLM driver (the LLM client is built
// here, not inside claimsdiscover, so the package stays client-free —
// mirrors newProtocolInferDriver).
func newClaimsDriver() (*ai.Driver, error) {
	gcfg := config.Load()
	projCfg, err := project.LoadFromFile(filepath.Join(".", ".cerberus", "project.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load project.yaml: %w", err)
	}
	client, err := llm.NewClientWithConfig(llm.ClientConfig{
		Model:      projCfg.Settings.AIBudget.Model,
		APIKey:     gcfg.LLMAPIKey,
		BaseURL:    gcfg.LLMBaseURL,
		AuthScheme: gcfg.LLMAuthScheme,
	})
	if err != nil {
		return nil, fmt.Errorf("build llm client: %w", err)
	}
	total, perCall := projCfg.Settings.AIBudget.SessionTotalTokens, projCfg.Settings.AIBudget.PerCallLimit
	if total <= 0 {
		total = 200000
	}
	if perCall <= 0 {
		perCall = 10000
	}
	return ai.NewDriver(client, ai.NewTokenBudget(total, perCall)), nil
}
