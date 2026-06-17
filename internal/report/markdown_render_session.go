package report

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/store"
)

// renderSessionMetadata renders the session metadata table
func renderSessionMetadata(b *strings.Builder, s *store.Session) {
	b.WriteString("## Session\n\n")
	b.WriteString("| Field | Value |\n")
	b.WriteString("|-------|-------|\n")
	fmt.Fprintf(b, "| **ID** | `%s` |\n", s.ID)
	fmt.Fprintf(b, "| **Goal** | %s |\n", s.Goal)
	fmt.Fprintf(b, "| **Status** | %s |\n", statusEmoji(s.Status))
	if s.ProjectName != "" {
		fmt.Fprintf(b, "| **Project** | %s |\n", s.ProjectName)
	}
	fmt.Fprintf(b, "| **Started** | %s |\n", s.StartedAt)
	if s.FinishedAt != "" {
		fmt.Fprintf(b, "| **Finished** | %s |\n", s.FinishedAt)
	}
	b.WriteString("\n")
}
