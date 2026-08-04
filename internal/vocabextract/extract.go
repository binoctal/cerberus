package vocabextract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ErrNodeRequired is returned when node is not discoverable on PATH. Discovery is
// a development-time tool; the production cerberus binary stays pure-Go.
var ErrNodeRequired = errors.New("vocabextract: node not found on PATH (required for TS discovery)")

// Extract writes the bundled extractor.mjs + package.json to a temp dir,
// installs ts-morph if missing, and runs `node extractor.mjs <sourcePath>`.
// It returns the extractor's stdout (a JSON object with an `edges` array).
func Extract(ctx context.Context, sourcePath string) (json.RawMessage, error) {
	if _, err := exec.LookPath("node"); err != nil {
		return nil, ErrNodeRequired
	}
	dir, err := os.MkdirTemp("", "cerberus-vocab-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, "extractor.mjs"), []byte(extractorSrc), 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(packageJSON), 0644); err != nil {
		return nil, err
	}
	if err := npmInstall(ctx, dir); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return nil, err
	}
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "node", "extractor.mjs", abs)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("vocabextract: node run: %w", err)
	}
	return json.RawMessage(stdout.Bytes()), nil
}

// npmInstall runs `npm install` (silent) if node_modules is absent.
func npmInstall(ctx context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err == nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "npm", "install", "--silent", "--no-audit", "--no-fund")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	if out, err := cmd.Output(); err != nil {
		return fmt.Errorf("vocabextract: npm install: %w: %s", err, out)
	}
	return nil
}
