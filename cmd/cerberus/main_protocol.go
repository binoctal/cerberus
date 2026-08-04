package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/protocoldiscover"
	"github.com/binoctal/cerberus/internal/vocabextract"
)

var (
	protocolInferName    string
	protocolInferFrom    string
	protocolInferService string
	protocolInferDryRun  bool
	protocolInferSamples int
)

// protocolCmd is the parent for protocol-related subcommands.
func protocolCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "protocol",
		Short: "Authoring aids for WS protocol declarations",
	}
	cmd.AddCommand(protocolInferCmd())
	cmd.AddCommand(protocolVocabularyCmd())
	return cmd
}

var (
	protocolVocabName string
	protocolVocabFrom string
	protocolVocabDry  bool
)

func protocolVocabularyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vocabulary",
		Short: "Extract a WS routing vocabulary from a TypeScript source file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProtocolVocabulary(cmd.Context(), ".", protocolVocabFrom, protocolVocabName,
				protocolVocabDry, promptConfirm(os.Stdin, os.Stdout))
		},
	}
	cmd.Flags().StringVar(&protocolVocabName, "name", "", "vocab file name (.cerberus/vocab/<name>.vocab.yaml); required")
	cmd.Flags().StringVar(&protocolVocabFrom, "from", "", "path to the TS source file; required")
	cmd.Flags().BoolVar(&protocolVocabDry, "dry-run", false, "print the draft without writing")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

// runProtocolVocabulary is the testable core. It extracts a vocabulary via
// vocabextract.Extract, hashes the source file (SHA-256), prints a draft, and
// on confirmation writes .cerberus/vocab/<name>.vocab.yaml. --dry-run never
// writes.
func runProtocolVocabulary(ctx context.Context, workDir, sourcePath, name string, dryRun bool, confirm func(string) bool) error {
	if err := project.CheckProtocolRefName(name); err != nil {
		return fmt.Errorf("--name: %w", err)
	}
	raw, err := vocabextract.Extract(ctx, sourcePath)
	if err != nil {
		return err
	}
	var extracted struct {
		Edges []project.VocabEdge `json:"edges"`
	}
	if err := json.Unmarshal(raw, &extracted); err != nil {
		return fmt.Errorf("parse extractor output: %w", err)
	}
	srcPath := sourcePath
	if !filepath.IsAbs(srcPath) {
		srcPath = filepath.Join(workDir, srcPath)
	}
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("hash source: %w", err)
	}
	sum := sha256.Sum256(srcData)
	vocab := &project.Vocabulary{
		Source: project.VocabSource{
			Files:       []project.VocabFile{{Path: sourcePath, Hash: hex.EncodeToString(sum[:])}},
			ProtocolRef: name,
		},
		Edges: extracted.Edges,
	}
	block, _ := yaml.Marshal(vocab)
	fmt.Printf("Draft vocabulary %q (%d edges):\n%s\n", name, len(vocab.Edges), string(block))
	if dryRun {
		return nil
	}
	outPath := filepath.Join(workDir, ".cerberus", "vocab", name+".vocab.yaml")
	rel := filepath.Join(".cerberus", "vocab", name+".vocab.yaml")
	question := fmt.Sprintf("Write draft to %s? [y/N]", rel)
	if _, statErr := os.Stat(outPath); statErr == nil {
		question = fmt.Sprintf("%s already exists. Overwrite? [y/N]", rel)
	}
	if confirm == nil || !confirm(question) {
		fmt.Println("aborted; no changes written")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outPath, block, 0644)
}

func protocolInferCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infer",
		Short: "Draft a WS protocol description from docs/examples for human review",
		RunE: func(cmd *cobra.Command, args []string) error {
			driver, err := newProtocolInferDriver()
			if err != nil {
				return err
			}
			return runProtocolInfer(cmd.Context(), ".", driver, protocolInferOpts{
				Name:    protocolInferName,
				From:    protocolInferFrom,
				Service: protocolInferService,
				DryRun:  protocolInferDryRun,
				Samples: protocolInferSamples,
				confirm: promptConfirm(os.Stdin, os.Stdout),
			})
		},
	}
	cmd.Flags().StringVar(&protocolInferName, "name", "", "protocol file name (.cerberus/protocols/<name>.yaml); plain name, required")
	cmd.Flags().StringVar(&protocolInferFrom, "from", "", "path to a doc/example file or dir to infer from (required)")
	cmd.Flags().StringVar(&protocolInferService, "service", "", "service the protocol targets (default: first service)")
	cmd.Flags().BoolVar(&protocolInferDryRun, "dry-run", false, "print the draft without writing")
	cmd.Flags().IntVar(&protocolInferSamples, "samples", protocoldiscover.DefaultInferSamples, "number of drafts to run (best-of-N absorbs variance)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

// protocolInferOpts holds parsed command inputs. confirm abstracts the y/N
// prompt so tests inject a deterministic answerer (mirrors authDiscoverOpts).
type protocolInferOpts struct {
	Name    string
	From    string
	Service string
	DryRun  bool
	Samples int
	confirm func(prompt string) bool
}

// runProtocolInfer is the testable core. It loads project.yaml, drafts a
// Protocol via protocoldiscover.Infer (which validates before returning),
// prints it, and on confirmation writes it to .cerberus/protocols/<name>.yaml.
// ErrNoProtocol is reported via stdout, not returned. It never writes without
// confirmation unless --dry-run (which never writes).
func runProtocolInfer(ctx context.Context, workDir string, driver *ai.Driver, opts protocolInferOpts) error {
	if err := project.CheckProtocolRefName(opts.Name); err != nil {
		return fmt.Errorf("--name: %w", err)
	}
	cfgPath := filepath.Join(workDir, ".cerberus", "project.yaml")
	cfg, err := project.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("load project.yaml: %w", err)
	}
	service := opts.Service
	if service == "" && len(cfg.Services) > 0 {
		service = cfg.Services[0].Name
	}
	inputs, err := readInputs(filepath.Join(workDir, opts.From))
	if err != nil {
		return fmt.Errorf("read --from: %w", err)
	}
	samples := opts.Samples
	if samples <= 0 {
		samples = protocoldiscover.DefaultInferSamples
	}
	p, err := protocoldiscover.Infer(ctx, driver, cfg, service, inputs, samples)
	if errors.Is(err, protocoldiscover.ErrNoProtocol) {
		fmt.Println("no WebSocket protocol found in the provided inputs")
		return nil
	}
	if err != nil {
		return err
	}
	// The written YAML carries only protocol shape + credential_ref names
	// (actor names, not tokens); marshalling a *project.Protocol cannot leak
	// credential values because the struct has no field for them.
	block, _ := yaml.Marshal(p)
	fmt.Printf("Draft protocol %q:\n%s\n", opts.Name, string(block))
	if opts.DryRun {
		return nil
	}
	outPath := filepath.Join(workDir, ".cerberus", "protocols", opts.Name+".yaml")
	rel := filepath.Join(".cerberus", "protocols", opts.Name+".yaml")
	question := fmt.Sprintf("Write draft to %s? [y/N]", rel)
	if _, statErr := os.Stat(outPath); statErr == nil {
		question = fmt.Sprintf("%s already exists. Overwrite? [y/N]", rel)
	}
	if opts.confirm == nil || !opts.confirm(question) {
		fmt.Println("aborted; no changes written")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outPath, block, 0644)
}

// readInputs reads a file or enumerates a dir into SourceFiles (text only).
// Binary and unrecognized files are skipped so the prompt stays text-shaped.
func readInputs(path string) ([]protocoldiscover.SourceFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []protocoldiscover.SourceFile{{Path: path, Content: string(data)}}, nil
	}
	var out []protocoldiscover.SourceFile
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !isTextFile(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(path, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, protocoldiscover.SourceFile{Path: e.Name(), Content: string(data)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no readable text files under %s", path)
	}
	return out, nil
}

// isTextFile restricts prompt inputs to source/doc formats the model can read
// as text. Keeping the allowlist narrow avoids base64-ing binaries into prompts.
func isTextFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".markdown", ".txt", ".json", ".yaml", ".yml", ".ts", ".js", ".go", ".py":
		return true
	}
	return false
}

// newProtocolInferDriver mirrors newAuthDiscoverDriver: the LLM client is
// built here, not inside protocoldiscover, so the package stays client-free
// and tests inject a mock driver through runProtocolInfer.
func newProtocolInferDriver() (*ai.Driver, error) {
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
