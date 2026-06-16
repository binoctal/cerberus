package types

import (
	"fmt"
	"time"
)

// MCPResult represents an MCP server call result.
type MCPResult struct {
	OK      bool          `json:"success"`
	Body    string        `json:"body"`
	Latency time.Duration `json:"duration"`
	Err     string        `json:"error,omitempty"`
}

func (r MCPResult) Success() bool           { return r.OK }
func (r MCPResult) Duration() time.Duration { return r.Latency }
func (r MCPResult) Summary() string {
	status := "error"
	if r.OK {
		status = "ok"
	}
	return fmt.Sprintf("MCP %s (%s)", status, r.Latency)
}
func (r MCPResult) Evidence() EvidenceData {
	return EvidenceData{Type: "mcp_response", Content: truncate(r.Body, 10000)}
}
