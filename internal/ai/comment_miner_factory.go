package ai

import "regexp"

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
