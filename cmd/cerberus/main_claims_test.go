package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

// claimsProjectFixture declares one real-process actor whose name doubles as a
// surface token for triage, mirroring the dogfood shape.
const claimsProjectFixture = `project:
  name: claims-test
actors:
  - name: bridge-pty-1
    credentials: {}
    fidelity: real-process
    process:
      workdir: "../../bridge"
      start: ["./build/open-agents-bridge", "start"]
`

// mockClaimsDriver wires a canned claims_extract payload (mockProtocolDriver
// idiom).
func mockClaimsDriver(claims []any) *ai.Driver {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("default", []llm.ToolCall{
		{Name: "claims_extract", Input: map[string]any{"claims": claims}},
	})
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}

func cannedClaim(id, text string) map[string]any {
	return map[string]any{"id": id, "text": text}
}

// seedClaimsProject writes .cerberus/project.yaml plus a README doc source and
// returns the README's relative path.
func seedClaimsProject(t *testing.T, workDir, readme string) string {
	t.Helper()
	writeProtocolProjectYAML(t, workDir, claimsProjectFixture)
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte(readme), 0644); err != nil {
		t.Fatal(err)
	}
	return "README.md"
}

func readLedger(t *testing.T, workDir string) *project.ClaimsFile {
	t.Helper()
	cf, err := project.LoadClaims(workDir)
	require.NoError(t, err)
	require.NotNil(t, cf)
	return cf
}

func TestRunClaimsExtract_WritesLedger(t *testing.T) {
	workDir := t.TempDir()
	from := seedClaimsProject(t, workDir, "# readme\nbridge-pty-1 devices pair via the dev backdoor\n")
	var out bytes.Buffer
	err := runClaimsExtract(context.Background(), workDir, mockClaimsDriver([]any{
		cannedClaim("bridge-pairing", "bridge-pty-1 devices pair via the dev backdoor"),
		cannedClaim("delight", "The product feels delightful"),
	}), claimsExtractOpts{From: from, Max: 15}, &out)
	require.NoError(t, err)
	cf := readLedger(t, workDir)
	require.Len(t, cf.Claims, 2)
	// Source hash recorded per file (vocab convention).
	require.Len(t, cf.Source.Files, 1)
	assert.Equal(t, from, cf.Source.Files[0].Path)
	assert.True(t, strings.HasPrefix(cf.Source.Files[0].Hash, "sha256:"))
}

func TestRunClaimsExtract_MergesPreservingManualChannels(t *testing.T) {
	workDir := t.TempDir()
	from := seedClaimsProject(t, workDir, "# readme\n")
	first := mockClaimsDriver([]any{
		cannedClaim("keep-me", "bridge-pty-1 devices pair"),
		cannedClaim("stale-me", "old promise"),
	})
	require.NoError(t, runClaimsExtract(context.Background(), workDir, first,
		claimsExtractOpts{From: from}, &bytes.Buffer{}))
	// Manually triage: exemption + critical override.
	cf := readLedger(t, workDir)
	for i := range cf.Claims {
		if cf.Claims[i].ID == "stale-me" {
			cf.Claims[i].StatusAnnotation = "wont-test(intentional)"
		}
	}
	block, err := os.ReadFile(filepath.Join(workDir, ".cerberus", "claims.yaml"))
	require.NoError(t, err)
	// Write via re-marshal after in-memory edit: simpler to rewrite the file.
	_ = block
	require.NoError(t, writeClaimsFile(workDir, cf))

	// Re-extract without stale-me in the draft.
	second := mockClaimsDriver([]any{cannedClaim("keep-me", "bridge-pty-1 devices pair")})
	var out bytes.Buffer
	require.NoError(t, runClaimsExtract(context.Background(), workDir, second,
		claimsExtractOpts{From: from}, &out))
	merged := readLedger(t, workDir)
	byID := map[string]project.Claim{}
	for _, c := range merged.Claims {
		byID[c.ID] = c
	}
	// Append-only: stale-me survives without --prune, with its exemption.
	require.Len(t, merged.Claims, 2)
	assert.Equal(t, "wont-test(intentional)", byID["stale-me"].StatusAnnotation)
	assert.Contains(t, out.String(), "stale-me")

	// With --prune the stale id is dropped.
	third := mockClaimsDriver([]any{cannedClaim("keep-me", "bridge-pty-1 devices pair")})
	require.NoError(t, runClaimsExtract(context.Background(), workDir, third,
		claimsExtractOpts{From: from, Prune: true}, &bytes.Buffer{}))
	pruned := readLedger(t, workDir)
	require.Len(t, pruned.Claims, 1)
	assert.Equal(t, "keep-me", pruned.Claims[0].ID)
}

func TestRunClaimsExtract_NoClaimsWritesNothing(t *testing.T) {
	workDir := t.TempDir()
	from := seedClaimsProject(t, workDir, "# readme\n")
	err := runClaimsExtract(context.Background(), workDir, mockClaimsDriver(nil),
		claimsExtractOpts{From: from}, &bytes.Buffer{})
	require.NoError(t, err)
	_, statErr := os.Stat(filepath.Join(workDir, ".cerberus", "claims.yaml"))
	assert.True(t, os.IsNotExist(statErr), "no claims extracted must not write a ledger")
}

func TestRunClaimsList_RendersLedger(t *testing.T) {
	workDir := t.TempDir()
	from := seedClaimsProject(t, workDir, "# readme\n")
	require.NoError(t, runClaimsExtract(context.Background(), workDir,
		mockClaimsDriver([]any{cannedClaim("bridge-pairing", "bridge-pty-1 devices pair")}),
		claimsExtractOpts{From: from}, &bytes.Buffer{}))
	var out bytes.Buffer
	require.NoError(t, runClaimsList(workDir, &out))
	body := out.String()
	assert.Contains(t, body, "bridge-pairing")
	assert.Contains(t, body, "critical")
}

// newClaimsStore opens a migrated store at a fresh temp path and returns both,
// so runClaimsCheck can be pointed at the same file.
func newClaimsStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "check.db")
	s, err := store.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), migrationDir(t)))
	return s, dbPath
}

func TestRunClaimsCheck_EmptyStorePrintsLedger(t *testing.T) {
	workDir := t.TempDir()
	from := seedClaimsProject(t, workDir, "# readme\n")
	require.NoError(t, runClaimsExtract(context.Background(), workDir,
		mockClaimsDriver([]any{cannedClaim("bridge-pairing", "bridge-pty-1 devices pair")}),
		claimsExtractOpts{From: from}, &bytes.Buffer{}))
	_, dbPath := newClaimsStore(t)
	var out bytes.Buffer
	require.NoError(t, runClaimsCheck(context.Background(), workDir, dbPath, &out))
	body := out.String()
	assert.Contains(t, body, "bridge-pairing")
	assert.Contains(t, body, "no verdicts")
}

func TestRunClaimsCheck_PrintsLatestSessionVerdicts(t *testing.T) {
	workDir := t.TempDir()
	from := seedClaimsProject(t, workDir, "# readme\n")
	require.NoError(t, runClaimsExtract(context.Background(), workDir,
		mockClaimsDriver([]any{cannedClaim("bridge-pairing", "bridge-pty-1 devices pair")}),
		claimsExtractOpts{From: from}, &bytes.Buffer{}))
	s, dbPath := newClaimsStore(t)
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "run", "goal", "claims-test")
	require.NoError(t, err)
	summary := &session.SessionSummary{
		ClaimsVerdicts: []session.ClaimVerdict{{
			Claim:  project.Claim{ID: "bridge-pairing", Text: "bridge-pty-1 devices pair"},
			Status: session.ClaimProven,
			Cases:  []string{"case-1"},
		}},
	}
	require.NoError(t, s.UpdateSessionStats(ctx, sess.ID, 100, summary))

	var out bytes.Buffer
	require.NoError(t, runClaimsCheck(ctx, workDir, dbPath, &out))
	body := out.String()
	assert.Contains(t, body, "bridge-pairing: proven")
}

func TestAutoExtractClaims_CreatesLedger(t *testing.T) {
	workDir := t.TempDir()
	seedClaimsProject(t, workDir, "# readme\nbridge-pty-1 devices pair via the dev backdoor\n")
	cfg, err := project.LoadFromFile(filepath.Join(workDir, ".cerberus", "project.yaml"))
	require.NoError(t, err)
	var logged string
	ok, err := autoExtractClaims(context.Background(), workDir, cfg, mockClaimsDriver([]any{
		cannedClaim("bridge-pairing", "bridge-pty-1 devices pair via the dev backdoor"),
		cannedClaim("delight", "The product feels delightful"),
	}), func(msg string) { logged = msg })
	require.NoError(t, err)
	assert.True(t, ok)
	cf := readLedger(t, workDir)
	require.Len(t, cf.Claims, 2)
	assert.Equal(t, "claims ledger extracted (2 claims, 1 critical)", logged)
}

func TestAutoExtractClaims_SkipsWhenLedgerExists(t *testing.T) {
	workDir := t.TempDir()
	seedClaimsProject(t, workDir, "# readme\n")
	// Pre-existing ledger: extraction must not run (no driver response preset,
	// so a call would drift) and the file must stay untouched.
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, ".cerberus"), 0755))
	orig := []byte("source:\n  files: []\nclaims:\n  - id: manual\n    text: manual claim\n")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".cerberus", "claims.yaml"), orig, 0644))
	cfg, err := project.LoadFromFile(filepath.Join(workDir, ".cerberus", "project.yaml"))
	require.NoError(t, err)
	mock := llm.NewMockClient(nil) // no preset: a drift would surface as ErrNoClaims
	ok, err := autoExtractClaims(context.Background(), workDir, cfg,
		ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000)), func(string) {})
	require.NoError(t, err)
	assert.False(t, ok)
	after, err := os.ReadFile(filepath.Join(workDir, ".cerberus", "claims.yaml"))
	require.NoError(t, err)
	assert.Equal(t, string(orig), string(after))
}

func TestAutoExtractClaims_NoDocSourceIsNoOp(t *testing.T) {
	workDir := t.TempDir()
	writeProtocolProjectYAML(t, workDir, claimsProjectFixture)
	cfg, err := project.LoadFromFile(filepath.Join(workDir, ".cerberus", "project.yaml"))
	require.NoError(t, err)
	mock := llm.NewMockClient(nil)
	ok, err := autoExtractClaims(context.Background(), workDir, cfg,
		ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000)), func(string) {})
	require.NoError(t, err)
	assert.False(t, ok)
	_, statErr := os.Stat(filepath.Join(workDir, ".cerberus", "claims.yaml"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestAutoExtractClaims_ServiceRepoReadme(t *testing.T) {
	workDir := t.TempDir()
	writeProtocolProjectYAML(t, workDir, claimsProjectFixture)
	// No README under the project dir; the actor's process workdir points at
	// <tmp>/repo/bridge, so the repo root README is one level up.
	repoBridge := filepath.Join(workDir, "repo", "bridge")
	require.NoError(t, os.MkdirAll(repoBridge, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "repo", "README.md"),
		[]byte("# repo readme\n"), 0644))
	fixture := strings.Replace(claimsProjectFixture, `"../../bridge"`, workDir+"/repo/bridge", 1)
	writeProtocolProjectYAML(t, workDir, fixture)
	cfg, err := project.LoadFromFile(filepath.Join(workDir, ".cerberus", "project.yaml"))
	require.NoError(t, err)
	ok, err := autoExtractClaims(context.Background(), workDir, cfg,
		mockClaimsDriver([]any{cannedClaim("bridge-pairing", "bridge-pty-1 devices pair")}),
		func(string) {})
	require.NoError(t, err)
	assert.True(t, ok)
	cf := readLedger(t, workDir)
	assert.Len(t, cf.Claims, 1)
	require.Len(t, cf.Source.Files, 1)
	assert.True(t, strings.HasSuffix(cf.Source.Files[0].Path, "README.md"),
		"source path should point at the repo README: %s", cf.Source.Files[0].Path)
}

func TestClaimsCmd_Tree(t *testing.T) {
	root := claimsCmd()
	assert.Equal(t, "claims", root.Use)
	var extract, list, check *cobra.Command
	for _, c := range root.Commands() {
		switch c.Use {
		case "extract":
			extract = c
		case "list":
			list = c
		case "check":
			check = c
		}
	}
	require.NotNil(t, extract, "claims must register an extract subcommand")
	require.NotNil(t, list, "claims must register a list subcommand")
	require.NotNil(t, check, "claims must register a check subcommand")
	for _, f := range []string{"from", "max", "prune"} {
		assert.NotNil(t, extract.Flags().Lookup(f), "extract missing flag %q", f)
	}
}
