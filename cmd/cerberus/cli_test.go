package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrationDir returns the absolute path to project migrations directory.
func migrationDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../migrations")
	require.NoError(t, err)
	return abs
}

// captureStdout runs f while capturing os.Stdout and returns the output.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	f()

	require.NoError(t, w.Close())
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

// setupReportTest seeds a temp DB with a completed session and returns
// (dbPath, sessionID). It also sets CERBERUS_MIGRATION_DIR env var.
func setupReportTest(t *testing.T, goal string) (string, string) {
	t.Helper()
	t.Setenv("CERBERUS_MIGRATION_DIR", migrationDir(t))

	tmpFile := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(tmpFile)
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), migrationDir(t)))

	sess, err := s.CreateSession(context.Background(), "run", goal, "test-project")
	require.NoError(t, err)
	stats := `{"goal":"` + goal + `","passed":1,"failed":0,"skipped":0,"total":1,"coverage_pct":100}`
	require.NoError(t, s.UpdateSessionStats(context.Background(), sess.ID, 100, stats))
	require.NoError(t, s.UpdateSessionStatus(context.Background(), sess.ID, "completed"))
	_ = s.Close()

	return tmpFile, sess.ID
}

func TestVersionCmd_CobraPipeline(t *testing.T) {
	output := captureStdout(t, func() {
		cmd := versionCmd()
		cmd.Run(cmd, []string{})
	})
	assert.Contains(t, output, "cerberus")
}

func TestReportCmd_NoSession(t *testing.T) {
	t.Setenv("CERBERUS_MIGRATION_DIR", migrationDir(t))
	tmpFile := filepath.Join(t.TempDir(), "empty.db")
	s, err := store.New(tmpFile)
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), migrationDir(t)))
	_ = s.Close()

	origDB, origSession, origFormat := dbFlag, sessionFlag, formatFlag
	dbFlag = tmpFile
	sessionFlag = "nonexistent-session-id"
	formatFlag = "markdown"
	t.Cleanup(func() {
		dbFlag, sessionFlag, formatFlag = origDB, origSession, origFormat
	})

	cmd := reportCmd()
	err = cmd.RunE(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")
}

func TestReportCmd_Markdown(t *testing.T) {
	dbPath, sessID := setupReportTest(t, "markdown goal")

	origDB, origSession, origFormat, origOutput := dbFlag, sessionFlag, formatFlag, outputFlag
	dbFlag = dbPath
	sessionFlag = sessID
	formatFlag = "markdown"
	outputFlag = ""
	t.Cleanup(func() {
		dbFlag, sessionFlag, formatFlag, outputFlag = origDB, origSession, origFormat, origOutput
	})

	cmd := reportCmd()
	cmd.Flags().Set("session", sessID)
	cmd.Flags().Set("format", "markdown")

	var runErr error
	output := captureStdout(t, func() {
		runErr = cmd.RunE(cmd, []string{})
	})
	require.NoError(t, runErr)
	assert.Contains(t, output, sessID)
}

func TestReportCmd_JUnit(t *testing.T) {
	dbPath, sessID := setupReportTest(t, "junit goal")

	origDB, origSession, origFormat, origOutput := dbFlag, sessionFlag, formatFlag, outputFlag
	dbFlag = dbPath
	sessionFlag = sessID
	formatFlag = "junit"
	outputFlag = ""
	t.Cleanup(func() {
		dbFlag, sessionFlag, formatFlag, outputFlag = origDB, origSession, origFormat, origOutput
	})

	cmd := reportCmd()
	cmd.Flags().Set("session", sessID)
	cmd.Flags().Set("format", "junit")

	var runErr error
	output := captureStdout(t, func() {
		runErr = cmd.RunE(cmd, []string{})
	})
	require.NoError(t, runErr)
	assert.Contains(t, output, "testsuites")
	assert.Contains(t, output, "testsuite")
}

func TestReportCmd_HTML(t *testing.T) {
	dbPath, sessID := setupReportTest(t, "html goal")

	origDB, origSession, origFormat, origOutput := dbFlag, sessionFlag, formatFlag, outputFlag
	dbFlag = dbPath
	sessionFlag = sessID
	formatFlag = "html"
	outputFlag = ""
	t.Cleanup(func() {
		dbFlag, sessionFlag, formatFlag, outputFlag = origDB, origSession, origFormat, origOutput
	})

	cmd := reportCmd()
	cmd.Flags().Set("session", sessID)
	cmd.Flags().Set("format", "html")

	var runErr error
	output := captureStdout(t, func() {
		runErr = cmd.RunE(cmd, []string{})
	})
	require.NoError(t, runErr)
	assert.Contains(t, output, "<html")
}

func TestReportCmd_ToFile(t *testing.T) {
	dbPath, sessID := setupReportTest(t, "file goal")
	outPath := filepath.Join(t.TempDir(), "report.md")

	origDB, origSession, origFormat, origOutput := dbFlag, sessionFlag, formatFlag, outputFlag
	dbFlag = dbPath
	sessionFlag = sessID
	formatFlag = "markdown"
	outputFlag = outPath
	t.Cleanup(func() {
		dbFlag, sessionFlag, formatFlag, outputFlag = origDB, origSession, origFormat, origOutput
	})

	cmd := reportCmd()
	cmd.Flags().Set("session", sessID)
	cmd.Flags().Set("format", "markdown")
	cmd.Flags().Set("output", outPath)

	err := cmd.RunE(cmd, []string{})
	require.NoError(t, err)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), sessID)
}

func TestReportCmd_BadFormat(t *testing.T) {
	dbPath, sessID := setupReportTest(t, "bad format goal")

	origDB, origSession, origFormat, origOutput := dbFlag, sessionFlag, formatFlag, outputFlag
	dbFlag = dbPath
	sessionFlag = sessID
	formatFlag = "csv"
	outputFlag = ""
	t.Cleanup(func() {
		dbFlag, sessionFlag, formatFlag, outputFlag = origDB, origSession, origFormat, origOutput
	})

	cmd := reportCmd()
	cmd.Flags().Set("session", sessID)
	cmd.Flags().Set("format", "csv")

	err := cmd.RunE(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestInitCmd_CobraPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	cmd := initCmd()
	err = cmd.RunE(cmd, []string{})
	require.NoError(t, err)

	assert.FileExists(t, ".cerberus/project.yaml")
	assert.FileExists(t, ".cerberus/credentials.yaml")

	data, err := os.ReadFile(".cerberus/project.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(data), "services:")
}

func TestRootCmd_UnknownCommand(t *testing.T) {
	root := &cobra.Command{Use: "cerberus", Short: "test"}
	root.AddCommand(versionCmd())
	root.SetArgs([]string{"nonexistent"})
	root.SetErr(bytes.NewBuffer(nil))
	err := root.Execute()
	require.Error(t, err)
}
