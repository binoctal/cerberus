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
	"regexp"
	"sort"
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
	cmd.AddCommand(protocolUIVocabCmd())
	return cmd
}

var (
	protocolVocabName string
	protocolVocabFrom []string
	protocolVocabDry  bool
)

var (
	uiVocabRouter string
	uiVocabPages  string
	uiVocabNav    string
	uiVocabLocale string
)

// protocolUIVocabCmd proposes static UI display-promise assertions mined
// from real source (PageHeader title props resolved against the locale
// file, cross-checked against persistent nav chrome) — the grounded-
// extraction counterpart to `protocol vocabulary`'s WS/HTTP passes, built
// specifically to avoid the LLM-invents-plausible-but-absent-surface
// failure mode this project already hit for HTTP endpoints (see
// downgradeUnmodeledHTTPProbes in internal/head/scout/direct_planning.go).
// Print-only in v1: vocab.yaml's ui.assertions carries hand-curated,
// protocol-coupled, and non-PageHeader entries a blind write would
// clobber — committing a candidate stays a human (or agent) editing step.
func protocolUIVocabCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui-vocab",
		Short: "Propose UI display-promise vocab assertions from React PageHeader usage (print-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProtocolUIVocab(uiVocabRouter, uiVocabPages, uiVocabNav, uiVocabLocale, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&uiVocabRouter, "router", "", "path to the React router source (e.g. App.tsx) — required")
	cmd.Flags().StringVar(&uiVocabPages, "pages", "", "path to the page components directory — required")
	cmd.Flags().StringVar(&uiVocabNav, "nav", "", "path to the persistent layout/nav chrome source — required")
	cmd.Flags().StringVar(&uiVocabLocale, "locale", "", "path to the locale JSON (e.g. en.json) — required")
	for _, f := range []string{"router", "pages", "nav", "locale"} {
		_ = cmd.MarkFlagRequired(f)
	}
	return cmd
}

// runProtocolUIVocab is the testable core: run the extractor and print two
// YAML-ready blocks (safe candidates, flagged collisions) plus a one-line
// summary. It never writes to any file — the caller pastes safe candidates
// into vocab.yaml's ui.assertions by hand, the same review step already
// used for missions-conn-status et al.
func runProtocolUIVocab(routerFile, pagesDir, navFile, localeFile string, out io.Writer) error {
	res, err := vocabextract.ExtractUITitleCandidatesFromDisk(routerFile, pagesDir, navFile, localeFile)
	if err != nil {
		return err
	}
	slug := func(route string) string {
		s := strings.TrimPrefix(route, "/dashboard")
		s = strings.Trim(s, "/")
		s = strings.ReplaceAll(s, "/", "-")
		if s == "" {
			s = "home"
		}
		return s
	}
	_, _ = fmt.Fprintf(out, "# %d safe candidate(s) — paste into vocab.yaml's ui.assertions after review\n", len(res.Safe))
	for _, c := range res.Safe {
		_, _ = fmt.Fprintf(out, "- id: %s-title\n", slug(c.Route))
		_, _ = fmt.Fprintf(out, "  route: %s\n", c.Route)
		_, _ = fmt.Fprintf(out, "  target: \"text=%s\"\n", c.Text)
		_, _ = fmt.Fprintf(out, "  expectation: text_present\n")
		_, _ = fmt.Fprintf(out, "  timeout: 15\n")
		_, _ = fmt.Fprintf(out, "  # source: %s (%s)\n", c.SourceFile, c.I18nKey)
	}
	if len(res.Flagged) > 0 {
		_, _ = fmt.Fprintf(out, "\n# %d candidate(s) FLAGGED — text collides with persistent nav/layout chrome,\n", len(res.Flagged))
		_, _ = fmt.Fprintf(out, "# would pass on every page and prove nothing; needs a different selector or skip:\n")
		for _, c := range res.Flagged {
			_, _ = fmt.Fprintf(out, "#   %s (%s): %q — source: %s (%s)\n", c.Route, c.Component, c.Text, c.SourceFile, c.I18nKey)
		}
	}
	return nil
}

func protocolVocabularyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vocabulary",
		Short: "Extract a WS routing vocabulary and HTTP route surface from TypeScript source files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProtocolVocabulary(cmd.Context(), ".", protocolVocabFrom, protocolVocabName,
				protocolVocabDry, promptConfirm(os.Stdin, os.Stdout))
		},
	}
	cmd.Flags().StringVar(&protocolVocabName, "name", "", "vocab file name (.cerberus/vocab/<name>.vocab.yaml); required")
	cmd.Flags().StringArrayVar(&protocolVocabFrom, "from", nil, "path to a TS source file; repeatable (e.g. DO room.ts + Hono worker.ts merge into one vocab); required")
	cmd.Flags().BoolVar(&protocolVocabDry, "dry-run", false, "print the draft without writing")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

// runProtocolVocabulary is the testable core. It extracts a vocabulary from
// one or more source entries (repeated --from: a WS room entry and a Hono
// worker entry merge into one vocab), hashes every traversed source file
// (SHA-256), prints a draft, and on confirmation writes
// .cerberus/vocab/<name>.vocab.yaml. --dry-run never writes.
func runProtocolVocabulary(ctx context.Context, workDir string, sources []string, name string, dryRun bool, confirm func(string) bool) error {
	if err := project.CheckProtocolRefName(name); err != nil {
		return fmt.Errorf("--name: %w", err)
	}
	if len(sources) == 0 {
		return fmt.Errorf("--from: at least one source file required")
	}
	var (
		edges  []project.VocabEdge
		routes []project.VocabHTTPRoute
		// Cross-entry merge keys mirror the extractor's internal dedup:
		// edges by from|to|type|trigger, routes by method|path — spans union.
		edgeIdx  = map[string]int{}
		routeIdx = map[string]int{}
		// Traversed files in first-seen order; the extractor reports
		// absolute paths, entries fall back to workDir-joined paths.
		hashOrder []string
		fileSeen  = map[string]bool{}
	)
	seenFile := func(p string) {
		if !fileSeen[p] {
			fileSeen[p] = true
			hashOrder = append(hashOrder, p)
		}
	}
	for _, sourcePath := range sources {
		raw, err := vocabextract.Extract(ctx, sourcePath)
		if err != nil {
			return err
		}
		var extracted struct {
			Edges      []project.VocabEdge      `json:"edges"`
			HTTPRoutes []project.VocabHTTPRoute `json:"http_routes"`
			Files      []struct {
				Path string `json:"path"`
			} `json:"files"`
		}
		if err := json.Unmarshal(raw, &extracted); err != nil {
			return fmt.Errorf("parse extractor output: %w", err)
		}
		for _, e := range extracted.Edges {
			k := e.FromRole + "|" + e.ToRole + "|" + e.Type + "|" + e.Trigger
			if i, ok := edgeIdx[k]; ok {
				edges[i].Source.Spans = append(edges[i].Source.Spans, e.Source.Spans...)
			} else {
				edgeIdx[k] = len(edges)
				edges = append(edges, e)
			}
		}
		for _, r := range extracted.HTTPRoutes {
			k := r.Method + "|" + r.Path
			if i, ok := routeIdx[k]; ok {
				routes[i].Source.Spans = append(routes[i].Source.Spans, r.Source.Spans...)
			} else {
				routeIdx[k] = len(routes)
				routes = append(routes, r)
			}
		}
		if len(extracted.Files) > 0 {
			for _, f := range extracted.Files {
				seenFile(f.Path)
			}
		} else {
			srcPath := sourcePath
			if !filepath.IsAbs(srcPath) {
				srcPath = filepath.Join(workDir, srcPath)
			}
			seenFile(srcPath)
		}
	}
	var files []project.VocabFile
	for _, p := range hashOrder {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("hash source %s: %w", p, err)
		}
		sum := sha256.Sum256(data)
		files = append(files, project.VocabFile{Path: p, Hash: hex.EncodeToString(sum[:])})
	}
	outPath := filepath.Join(workDir, ".cerberus", "vocab", name+".vocab.yaml")
	// The previous vocab (when re-extracting) decides the effective auth
	// middleware list BEFORE per-route derivation runs.
	var prev *project.Vocabulary
	if p, perr := project.LoadVocabulary(outPath); perr == nil {
		prev = p
	}
	// Hand-deleted param chains (param_sources_off in the previous vocab)
	// must never resurrect: re-derivation skips vetoed params entirely —
	// the heuristic cannot distinguish "hand-deleted" from "never curated",
	// so the veto is the only durable record of the deletion.
	vetoedParam := map[string]bool{}
	if prev != nil {
		for _, r := range prev.HTTPRoutes {
			for _, p := range r.ParamSourcesOff {
				vetoedParam[r.Method+"|"+r.Path+"|"+p] = true
			}
		}
	}
	// Auth derivation (spec §1): auth = middlewares ∩ the service-level auth
	// list. The effective list is the previous vocab's hand-curated
	// http_auth_middlewares when present — anonymous gates (use:/api/*) match
	// no name regex, so the curated list is the only way their auth facts
	// flow. Only when no prior list exists does the (?i)auth|bearer|jwt name
	// heuristic supply it, and the matched set is emitted as before so a
	// human can review and override.
	effective := map[string]bool{}
	if prev != nil {
		for _, mw := range prev.HTTPAuthMiddlewares {
			effective[mw] = true
		}
	}
	authMw := map[string]bool{}
	authMwRe := regexp.MustCompile(`(?i)auth|bearer|jwt`)
	if len(effective) == 0 {
		for _, r := range routes {
			for _, mw := range r.Middlewares {
				if authMwRe.MatchString(mw) {
					authMw[mw] = true
				}
			}
		}
		for mw := range authMw {
			effective[mw] = true
		}
	}
	authMiddlewares := make([]string, 0, len(authMw))
	for mw := range authMw {
		authMiddlewares = append(authMiddlewares, mw)
	}
	sort.Strings(authMiddlewares)
	for i := range routes {
		switch {
		case len(routes[i].Middlewares) == 0:
			routes[i].Auth = "none"
		default:
			routes[i].Auth = "unknown"
			for _, mw := range routes[i].Middlewares {
				if effective[mw] {
					routes[i].Auth = "required"
					break
				}
			}
		}
		// Param-chain inference: a trailing :param whose param-free prefix
		// is a GET list route chains to that route, picking the first
		// record's id. Hand-set sources (none can exist on a first pass)
		// win over re-derivation via the preservation block below.
		for _, p := range pathParams(routes[i].Path) {
			if _, hand := routes[i].ParamSources[p]; hand {
				continue
			}
			if vetoedParam[routes[i].Method+"|"+routes[i].Path+"|"+p] {
				continue // param_sources_off: hand-deleted chain stays deleted
			}
			list := strings.TrimSuffix(routes[i].Path, "/"+p)
			if strings.Contains(list, ":") {
				continue
			}
			if !routeMethodPath(routes, "GET", list) {
				continue
			}
			if routes[i].ParamSources == nil {
				routes[i].ParamSources = map[string]project.VocabParamSource{}
			}
			routes[i].ParamSources[p] = project.VocabParamSource{Route: "GET " + list, Pick: "0.id"}
		}
	}
	vocab := &project.Vocabulary{
		Source: project.VocabSource{
			Files:       files,
			ProtocolRef: name,
		},
		Edges:               edges,
		HTTPRoutes:          routes,
		HTTPAuthMiddlewares: authMiddlewares,
	}
	// Re-extraction must not drop manually-annotated marks (partial/unsupported)
	// on edges that still exist, matched by (from_role, to_role, type). A blind
	// overwrite re-admits server-only edges to the coverage denominator, which
	// timeout-fail until the executor escalates. Extraction cannot know these
	// marks — they encode live-probe knowledge about the running server.
	if prev != nil {
		marks := make(map[string]project.VocabEdge, len(prev.Edges))
		for _, e := range prev.Edges {
			marks[e.FromRole+"|"+e.ToRole+"|"+e.Type] = e
		}
		for i := range vocab.Edges {
			if old, ok := marks[vocab.Edges[i].FromRole+"|"+vocab.Edges[i].ToRole+"|"+vocab.Edges[i].Type]; ok {
				vocab.Edges[i].Partial = old.Partial
				vocab.Edges[i].Unsupported = old.Unsupported
			}
		}
		// A hand-curated auth middleware list wins over the name heuristic.
		if len(prev.HTTPAuthMiddlewares) > 0 {
			vocab.HTTPAuthMiddlewares = prev.HTTPAuthMiddlewares
		}
		// The hand-curated UI surface is not derivable from source; a
		// re-extraction must never silently drop it.
		if prev.UI != nil {
			vocab.UI = prev.UI
		}
		// Route marks follow the same rule, keyed method|path. Hand-tuned
		// param chains and hand-set auth (spec §5: the judgment layer rides
		// the merge) win over re-derivation; middlewares/min_body are the
		// fact layer and always come back fresh above. Auth preservation
		// covers the judgment values none|required — "unknown" is the
		// not-determined marker (a first pass emits it everywhere), so
		// preserving it would freeze ignorance and block the curated-list
		// derivation from ever marking a route required.
		routeMarks := make(map[string]project.VocabHTTPRoute, len(prev.HTTPRoutes))
		for _, r := range prev.HTTPRoutes {
			routeMarks[r.Method+"|"+r.Path] = r
		}
		for i := range vocab.HTTPRoutes {
			if old, ok := routeMarks[vocab.HTTPRoutes[i].Method+"|"+vocab.HTTPRoutes[i].Path]; ok {
				vocab.HTTPRoutes[i].Partial = old.Partial
				vocab.HTTPRoutes[i].Unsupported = old.Unsupported
				if old.Auth == "none" || old.Auth == "required" {
					vocab.HTTPRoutes[i].Auth = old.Auth
				}
				for p, ps := range old.ParamSources {
					if vocab.HTTPRoutes[i].ParamSources == nil {
						vocab.HTTPRoutes[i].ParamSources = map[string]project.VocabParamSource{}
					}
					vocab.HTTPRoutes[i].ParamSources[p] = ps
				}
				vocab.HTTPRoutes[i].ParamSourcesOff = old.ParamSourcesOff
			}
		}
	}
	block, _ := yaml.Marshal(vocab)
	fmt.Printf("Draft vocabulary %q (%d edges, %d http routes):\n%s\n", name, len(vocab.Edges), len(vocab.HTTPRoutes), string(block))
	if dryRun {
		return nil
	}
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

// pathParams lists a route path's :name segments in order.
func pathParams(path string) []string {
	var params []string
	for _, seg := range strings.Split(path, "/") {
		if len(seg) > 1 && strings.HasPrefix(seg, ":") {
			params = append(params, seg)
		}
	}
	return params
}

// routeMethodPath reports whether a route with the exact method and path
// exists in the extracted set; param-chain targets must be real routes.
func routeMethodPath(routes []project.VocabHTTPRoute, method, path string) bool {
	for _, r := range routes {
		if r.Method == method && r.Path == path {
			return true
		}
	}
	return false
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
