package architecture

import (
	"os"
	"path/filepath"
	"strings"
)

// docFileCount holds counts of different documentation file types
type docFileCount struct {
	adrFiles   int
	designDocs int
	planDocs   int
}

// countDocumentationFiles walks the project to count documentation files
func (a *Analyzer) countDocumentationFiles() (docFileCount, error) {
	counts := docFileCount{}

	err := filepath.Walk(a.projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return a.handleDirectoryDuringWalk(path)
		}

		a.classifyDocumentationFile(path, &counts)
		return nil
	})

	return counts, err
}

// handleDirectoryDuringWalk decides whether to skip a directory
func (a *Analyzer) handleDirectoryDuringWalk(path string) error {
	// Skip vendor and hidden dirs
	if strings.Contains(path, "vendor") || strings.Contains(path, ".git") {
		return filepath.SkipDir
	}
	return nil
}

// classifyDocumentationFile checks if a file is documentation and updates counts
func (a *Analyzer) classifyDocumentationFile(path string, counts *docFileCount) {
	base := filepath.Base(path)
	lowerBase := strings.ToLower(base)

	if a.isADRFile(lowerBase) {
		counts.adrFiles++
	} else if a.isDesignDoc(lowerBase) {
		counts.designDocs++
	} else if a.isPlanDoc(lowerBase) {
		counts.planDocs++
	}
}

// isADRFile checks if filename matches ADR patterns
func (a *Analyzer) isADRFile(filename string) bool {
	return strings.HasSuffix(filename, "adr.md") ||
		strings.HasPrefix(filename, "adr-") ||
		strings.Contains(filename, "decision")
}

// isDesignDoc checks if filename matches design document patterns
func (a *Analyzer) isDesignDoc(filename string) bool {
	return strings.Contains(filename, "design") ||
		strings.Contains(filename, "spec") ||
		strings.Contains(filename, "architecture")
}

// isPlanDoc checks if filename matches implementation plan patterns
func (a *Analyzer) isPlanDoc(filename string) bool {
	return strings.Contains(filename, "plan") ||
		strings.Contains(filename, "implementation")
}
