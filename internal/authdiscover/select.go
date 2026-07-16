// Package authdiscover infers a declarative AuthFlow for an actor by reading
// the target service's source and asking the LLM. It is a one-shot authoring
// aid invoked by the `cerberus auth discover` command; it never runs at
// session time and never writes files itself.
package authdiscover

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SourceFile is a selected source file with the content the LLM will see.
type SourceFile struct {
	Path    string
	Content string
}

// Supported source extensions. Multi-language because login flows live in
// whatever stack the target service uses.
var sourceExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".py": true,
}

// Dirs never worth scanning for a login flow.
func isSkippedDir(dir string) bool {
	switch dir {
	case "vendor", "node_modules", "build", "dist", ".git", ".cerberus":
		return true
	}
	return false
}

// authKeywords raise a file's relevance. Lowercase substring match.
var authKeywords = []string{
	"login", "signin", "sign-in", "auth", "session", "jwt",
	"token", "bearer", "middleware", "route", "passport", "handler",
}

// maxFiles caps how many files are returned. A login flow spans a handful of
// files; more just burns prompt budget.
const maxFiles = 8

// maxBytes caps total selected content so the prompt fits the model window.
const maxBytes = 24000

type scored struct {
	file SourceFile
	hits int
}

// selectSourceFiles walks root, drops vendored/build dirs and non-source files,
// scores remaining files by auth-keyword hits, and returns the top files within
// a byte budget. Returns an error if root is missing.
func selectSourceFiles(root string) ([]SourceFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("code root %q not accessible: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("code root %q is not a directory", root)
	}
	var picks []scored
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if isSkippedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceExts[filepath.Ext(path)] {
			return nil
		}
		content, rErr := os.ReadFile(path)
		if rErr != nil {
			return nil // skip unreadable files rather than aborting
		}
		lower := strings.ToLower(string(content))
		hits := 0
		for _, kw := range authKeywords {
			hits += strings.Count(lower, kw)
		}
		picks = append(picks, scored{file: SourceFile{Path: path, Content: string(content)}, hits: hits})
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Rank by relevance (desc); stable tiebreak keeps deterministic order.
	sort.SliceStable(picks, func(i, j int) bool { return picks[i].hits > picks[j].hits })

	out := make([]SourceFile, 0, maxFiles)
	total := 0
	for _, p := range picks {
		if len(out) >= maxFiles || total >= maxBytes {
			break
		}
		if total+len(p.file.Content) > maxBytes {
			continue // skip oversized rather than truncating mid-file
		}
		out = append(out, p.file)
		total += len(p.file.Content)
	}
	return out, nil
}
