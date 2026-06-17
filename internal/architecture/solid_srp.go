package architecture

import (
	"go/token"
)

// analyzeSRP checks Single Responsibility Principle compliance
func (a *Analyzer) analyzeSRP(filePath string, report *ArchitectureReport) []ArchitectureIssue {
	issues := []ArchitectureIssue{}

	fset := token.NewFileSet()
	node, err := parseFileToAST(filePath, fset)
	if err != nil {
		return issues
	}

	// Phase 1: Collect functions and types
	decls := collectDeclarations(node)

	// Phase 2: Match responsibilities using pattern matcher
	matcher := NewPatternMatcher(responsibilityPatterns)
	allIdentifiers := append(decls.functions, decls.types...)
	responsibilities := matcher.collectMatches(allIdentifiers)

	// Phase 3: Report if multiple responsibilities found
	if len(responsibilities) > 1 {
		issue := reportSRPViolation(filePath, a.projectPath, responsibilities, report)
		issues = append(issues, issue)
	}

	return issues
}
