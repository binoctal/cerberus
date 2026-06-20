package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAnalyze_InterfaceDoesNotCrash reproduces a nil-pointer panic that
// occurred whenever the analyzed project declared any interface. processTypeSpec
// passed a nil *ast.File to countImplementations, which then dereferenced it.
// The test asserts Analyze returns cleanly for a project containing an interface.
func TestAnalyze_InterfaceDoesNotCrash(t *testing.T) {
	tmpDir := t.TempDir()
	src := []byte(`package demo

// Repository is a sample interface that used to trigger the crash.
type Repository interface {
	Find(id int) error
}

// repoImpl embeds the interface so implementation counting has work to do.
type repoImpl struct {
	Repository
}
`)
	if err := os.WriteFile(filepath.Join(tmpDir, "demo.go"), src, 0644); err != nil {
		t.Fatalf("write demo.go: %v", err)
	}

	analyzer := NewAnalyzer(tmpDir)
	// Must not panic / must not return an abstraction-analysis error.
	report, err := analyzer.Analyze()
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
}
