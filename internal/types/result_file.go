package types

import (
	"fmt"
	"time"
)

// FileResult represents a file system operation result.
type FileResult struct {
	OK      bool          `json:"success"`
	Path    string        `json:"path"`
	Content string        `json:"content,omitempty"`
	Exists  bool          `json:"exists,omitempty"`
	Matches []string      `json:"matches,omitempty"`
	Latency time.Duration `json:"duration"`
	Err     string        `json:"error,omitempty"`
}

func (r FileResult) Success() bool           { return r.OK }
func (r FileResult) Duration() time.Duration { return r.Latency }
func (r FileResult) Summary() string {
	if r.Err != "" {
		return fmt.Sprintf("file %s: %s", r.Path, r.Err)
	}
	return fmt.Sprintf("file %s OK (%s)", r.Path, r.Latency)
}
func (r FileResult) Evidence() EvidenceData {
	return EvidenceData{Type: "file_content", Content: truncate(r.Content, 10000)}
}
