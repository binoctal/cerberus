package ai

import (
	"fmt"
	"sort"
	"strings"
)

type ContextEntry struct {
	Source    string  `json:"source"`
	Content   string  `json:"content"`
	Relevance float64 `json:"relevance"`
}

func BuildContext(entries []ContextEntry) string {
	if len(entries) == 0 {
		return ""
	}

	sorted := make([]ContextEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Relevance > sorted[j].Relevance
	})

	var b strings.Builder
	for _, e := range sorted {
		fmt.Fprintf(&b, "[%s] %s\n", e.Source, e.Content)
	}
	return b.String()
}
