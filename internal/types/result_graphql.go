package types

import (
	"encoding/json"
	"fmt"
	"time"
)

// GraphQLResult represents a GraphQL query result.
type GraphQLResult struct {
	OK      bool           `json:"success"`
	URL     string         `json:"url"`
	Data    map[string]any `json:"data,omitempty"`
	Errors  []any          `json:"errors,omitempty"`
	Latency time.Duration  `json:"duration"`
	Err     string         `json:"error,omitempty"`
}

func (r GraphQLResult) Success() bool           { return r.OK }
func (r GraphQLResult) Duration() time.Duration { return r.Latency }
func (r GraphQLResult) Summary() string {
	status := "ok"
	if !r.OK {
		status = "error"
	}
	return fmt.Sprintf("graphql %s %s (%s)", status, r.URL, r.Latency)
}
func (r GraphQLResult) Evidence() EvidenceData {
	content, _ := json.Marshal(r.Data)
	return EvidenceData{Type: "graphql_response", Content: truncate(string(content), 10000)}
}
