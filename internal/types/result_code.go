package types

import (
	"encoding/json"
	"fmt"
	"time"
)

// CodeResult represents code analysis result.
type CodeResult struct {
	OK       bool          `json:"success"`
	Findings []CodeFinding `json:"findings"`
	Stats    CodeStats     `json:"stats"`
	Latency  time.Duration `json:"duration"`
	Err      string        `json:"error,omitempty"`
}

// CodeFinding represents a single code analysis finding.
type CodeFinding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

// CodeStats represents code analysis statistics.
type CodeStats struct {
	FilesAnalyzed int     `json:"files_analyzed"`
	SymbolCount   int     `json:"symbol_count"`
	Coverage      float64 `json:"coverage,omitempty"`
}

func (r CodeResult) Success() bool           { return r.OK }
func (r CodeResult) Duration() time.Duration { return r.Latency }
func (r CodeResult) Summary() string {
	return fmt.Sprintf("%d findings in %d files (%s)", len(r.Findings), r.Stats.FilesAnalyzed, r.Latency)
}
func (r CodeResult) Evidence() EvidenceData {
	content, _ := json.Marshal(r.Findings)
	return EvidenceData{Type: "code_findings", Content: string(content)}
}
