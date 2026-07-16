package authdiscover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSelectSourceFiles_RanksAuthRelevant(t *testing.T) {
	root := t.TempDir()
	// High-relevance: mentions login + jwt + token.
	writeFile(t, root, "src/auth/login.go", "package auth\n// login handler issues jwt token\n")
	// Low-relevance: unrelated route.
	writeFile(t, root, "src/routes/misc.go", "package routes\n// misc handler\n")
	// Ignored: vendored.
	writeFile(t, root, "vendor/lib/secret.go", "package lib\n// login token here\n")
	// Ignored: unsupported extension.
	writeFile(t, root, "src/readme.md", "# login token auth\n")

	got, err := selectSourceFiles(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want at least one selected file")
	}
	// Top hit must be the auth-relevant login.go, not misc.go.
	if filepath.Base(got[0].Path) != "login.go" {
		t.Fatalf("top file = %v, want login.go", got)
	}
	// Vendored and .md files must never appear.
	for _, f := range got {
		if strings.Contains(f.Path, "vendor") {
			t.Fatalf("vendored file selected: %s", f.Path)
		}
		if filepath.Ext(f.Path) == ".md" {
			t.Fatalf("non-source file selected: %s", f.Path)
		}
	}
}

func TestSelectSourceFiles_AdmitsMultipleLanguages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "svc/handler.ts", "// login token\n")
	writeFile(t, root, "svc/app.py", "# login token\n")
	got, err := selectSourceFiles(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	exts := map[string]bool{}
	for _, f := range got {
		exts[filepath.Ext(f.Path)] = true
	}
	if !exts[".ts"] || !exts[".py"] {
		t.Fatalf("want .ts and .py selected, got %v", exts)
	}
}

func TestSelectSourceFiles_MissingRoot(t *testing.T) {
	if _, err := selectSourceFiles(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("want error for missing root")
	}
}
