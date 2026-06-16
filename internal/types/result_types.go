package types

import "time"

// ExecutorResult is the interface for all execution results.
type ExecutorResult interface {
	Success() bool
	Duration() time.Duration
	Summary() string
	Evidence() EvidenceData
}

// EvidenceData holds evidence collected from execution.
type EvidenceData struct {
	Type     string `json:"type"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty"`
}

// truncate limits s to maxRunes.
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// joinStrings joins a slice of strings with a separator.
func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += sep + s
	}
	return result
}
