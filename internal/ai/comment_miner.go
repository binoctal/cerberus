package ai

import (
	"fmt"
	"regexp"
	"strings"
)

// CommentSource represents different types of comment sources
type CommentSource int

const (
	SingleLineComments CommentSource = iota
	MultiLineComments
	DocComments
	InlineComments
	PackageComments
	FileHeaderComments
	TODOComments
	FIXMEComments
	NOTEComments
	HACKComments
	WARNINGComments
)

// Comment represents a code comment with metadata
type Comment struct {
	Text        string
	Source      CommentSource
	FilePath    string
	LineNumber  int
	Semantics   *CommentSemantics
	ContextCode string // Surrounding code for context
}

// CommentSemantics represents the semantic meaning of a comment
type CommentSemantics struct {
	Purpose        string // "business_rule", "domain_logic", "validation", "workflow", "state_transition"
	BusinessTerm   string
	Constraints    []string
	EdgeCases      []string
	Examples       []string
	DecisionReason string // Why this approach was chosen
	Confidence     float64
}

// CommentMiner extracts business insights from code comments
type CommentMiner struct {
	patterns map[CommentSource][]*regexp.Regexp
}

// NewCommentMiner creates a new comment miner with pre-configured patterns
func NewCommentMiner() *CommentMiner {
	miner := &CommentMiner{
		patterns: make(map[CommentSource][]*regexp.Regexp),
	}

	// Single-line comment patterns
	miner.patterns[SingleLineComments] = []*regexp.Regexp{
		regexp.MustCompile(`//.*$`),
	}

	// Multi-line comment patterns
	miner.patterns[MultiLineComments] = []*regexp.Regexp{
		regexp.MustCompile(`/\*[\s\S]*?\*/`),
	}

	// Doc comment patterns (Go style)
	miner.patterns[DocComments] = []*regexp.Regexp{
		regexp.MustCompile(`//\s*(?:[\w\s]+:)\s*.*$`),
	}

	// Package comment patterns
	miner.patterns[PackageComments] = []*regexp.Regexp{
		regexp.MustCompile(`^//\s*Package\s+\w+`),
	}

	// TODO comment patterns
	miner.patterns[TODOComments] = []*regexp.Regexp{
		regexp.MustCompile(`(?i)TODO\b`),
		regexp.MustCompile(`(?i)\[TODO\]`),
	}

	// FIXME comment patterns
	miner.patterns[FIXMEComments] = []*regexp.Regexp{
		regexp.MustCompile(`(?i)FIXME\b`),
		regexp.MustCompile(`(?i)\[FIXME\]`),
	}

	// NOTE comment patterns
	miner.patterns[NOTEComments] = []*regexp.Regexp{
		regexp.MustCompile(`(?i)NOTE\b`),
		regexp.MustCompile(`(?i)\[NOTE\]`),
	}

	// HACK comment patterns
	miner.patterns[HACKComments] = []*regexp.Regexp{
		regexp.MustCompile(`(?i)HACK\b`),
		regexp.MustCompile(`(?i)\[HACK\]`),
	}

	// WARNING comment patterns
	miner.patterns[WARNINGComments] = []*regexp.Regexp{
		regexp.MustCompile(`(?i)WARNING\b`),
		regexp.MustCompile(`(?i)\[WARNING\]`),
		regexp.MustCompile(`(?i)XXX\b`),
	}

	return miner
}

// MineAggressively extracts comments with business relevance from code
// This is a comprehensive mining operation that:
// 1. Scans all files in the project
// 2. Extracts comments of all types
// 3. Analyzes semantics of business-critical comments
// 4. Returns structured comment data with confidence scores
func (m *CommentMiner) MineAggressively(projectPath string, files []string) ([]*Comment, error) {
	// Stub implementation - will be fully implemented in later tasks
	// For now, return empty slice
	return []*Comment{}, nil
}

// isBusinessComment determines if a comment contains business-relevant information
func (m *CommentMiner) isBusinessComment(text string) bool {
	businessKeywords := []string{
		"business", "rule", "policy", "constraint", "validation",
		"requirement", "spec", "workflow", "process", "domain",
		"entity", "state", "transition", "condition", "exception",
	}

	lowerText := strings.ToLower(text)
	for _, keyword := range businessKeywords {
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}

	return false
}

// extractBusinessTerms extracts business domain terms from comment text
func (m *CommentMiner) extractBusinessTerms(text string) []string {
	// Stub implementation - will extract domain-specific terms
	return []string{}
}

// inferPurpose infers the purpose of a comment based on its content
func (m *CommentMiner) inferPurpose(text string) string {
	lowerText := strings.ToLower(text)

	// Check for various patterns
	if strings.Contains(lowerText, "must") || strings.Contains(lowerText, "required") {
		return "validation"
	}
	if strings.Contains(lowerText, "when") || strings.Contains(lowerText, "then") {
		return "workflow"
	}
	if strings.Contains(lowerText, "state") || strings.Contains(lowerText, "transition") {
		return "state_transition"
	}
	if strings.Contains(lowerText, "business") || strings.Contains(lowerText, "rule") {
		return "business_rule"
	}

	return "general"
}

// calculateConfidence calculates a confidence score for comment semantics
func (m *CommentMiner) calculateConfidence(comment *Comment) float64 {
	// Base confidence
	confidence := 0.5

	// Increase confidence if comment has clear purpose
	if comment.Semantics != nil && comment.Semantics.Purpose != "" {
		confidence += 0.2
	}

	// Increase confidence if comment has business terms
	if comment.Semantics != nil && comment.Semantics.BusinessTerm != "" {
		confidence += 0.2
	}

	// Increase confidence for TODO/FIXME/NOTE comments
	if comment.Source == TODOComments || comment.Source == FIXMEComments || comment.Source == NOTEComments {
		confidence += 0.1
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// String returns the string representation of a comment source
func (s CommentSource) String() string {
	switch s {
	case SingleLineComments:
		return "SingleLine"
	case MultiLineComments:
		return "MultiLine"
	case DocComments:
		return "Doc"
	case InlineComments:
		return "Inline"
	case PackageComments:
		return "Package"
	case FileHeaderComments:
		return "FileHeader"
	case TODOComments:
		return "TODO"
	case FIXMEComments:
		return "FIXME"
	case NOTEComments:
		return "NOTE"
	case HACKComments:
		return "HACK"
	case WARNINGComments:
		return "WARNING"
	default:
		return "Unknown"
	}
}

// Validate checks if a comment is valid and complete
func (c *Comment) Validate() error {
	if c == nil {
		return fmt.Errorf("comment is nil")
	}

	if c.Text == "" {
		return fmt.Errorf("comment text is empty")
	}

	if c.FilePath == "" {
		return fmt.Errorf("comment file path is empty")
	}

	if c.LineNumber <= 0 {
		return fmt.Errorf("comment line number must be positive")
	}

	return nil
}
