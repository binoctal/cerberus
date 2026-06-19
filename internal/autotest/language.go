package autotest

import (
	"path/filepath"
)

// DetectLanguage infers the project language from source file extensions
// and marker files (package.json for Node, requirements.txt for Python).
func DetectLanguage(sourceFile string, markers map[string]bool) string {
	ext := filepath.Ext(sourceFile)
	switch ext {
	case ".go":
		return "go"
	case ".js", ".ts", ".jsx", ".tsx":
		return "node"
	case ".py":
		return "python"
	}
	if markers["package.json"] {
		return "node"
	}
	if markers["requirements.txt"] || markers["pyproject.toml"] {
		return "python"
	}
	return "go" // default
}

// ProviderForLanguage returns the coverage provider name for a language.
func ProviderForLanguage(lang string) string {
	switch lang {
	case "node":
		return "node"
	case "python":
		return "python"
	default:
		return "go"
	}
}
