package ai

import "regexp"

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
	Purpose        string   // "business_rule", "domain_logic", "validation", "workflow", "state_transition"
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
