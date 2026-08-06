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
	Type       string      `json:"type"`
	Content    string      `json:"content"`
	Encoding   string      `json:"encoding,omitempty"`
	Dimensions []Dimension `json:"dimensions,omitempty"`
}

// Dimension is one structured observation an executor recorded, classified by
// the assertable dimension it speaks to. Populate only the fields for its Kind.
// The judge decides whether the fact satisfies a claim; a dimension never
// carries a verdict, only observed facts.
type Dimension struct {
	Kind       string   `json:"kind"`                   // count|membership|ordering|value|presence
	Label      string   `json:"label"`                  // human/LLM-readable
	Recipients []string `json:"recipients,omitempty"`   // membership: connections that received
	Sender     string   `json:"sender,omitempty"`       // membership: connection that sent
	Excluded   *bool    `json:"excluded,omitempty"`     // membership: only set when actively probed
	Count      int      `json:"count,omitempty"`        // count
	Value      string   `json:"value,omitempty"`        // value: "status=200", "approved=true"
	Present    *bool    `json:"present,omitempty"`      // presence
	Order      []string `json:"order,omitempty"`        // ordering
	Note       string   `json:"note,omitempty"`         // short supplement, not the primary signal
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
