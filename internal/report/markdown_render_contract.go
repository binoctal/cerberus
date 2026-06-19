package report

import (
	"fmt"
	"strings"
)

// renderContractSection renders the coverage contract and assessment if present
func renderContractSection(b *strings.Builder, data *ReportData) {
	if data.Summary == nil || data.Summary.Contract == nil {
		return
	}

	contract := data.Summary.Contract
	b.WriteString("## Coverage Contract\n\n")

	// Contract metadata
	b.WriteString("| Field | Value |\n")
	b.WriteString("|-------|-------|\n")
	fmt.Fprintf(b, "| **Depth** | %s |\n", contract.Depth)
	fmt.Fprintf(b, "| **Scope** | %s |\n", joinStrings(contract.Scope, ", "))
	fmt.Fprintf(b, "| **Path Types** | %s |\n", joinStrings(contract.PathTypes, ", "))
	fmt.Fprintf(b, "| **Error Scope** | %s |\n", joinStrings(contract.ErrorScope, ", "))
	fmt.Fprintf(b, "| **Boundaries** | %s |\n", joinStrings(contract.Boundaries, ", "))

	// Coverage gate
	fmt.Fprintf(b, "| **Coverage Gate** | %s: line %.0f%%, branch %.0f%% |\n",
		contract.CoverageGate.Module,
		contract.CoverageGate.LineThreshold*100,
		contract.CoverageGate.BranchThreshold*100)

	// Invariants
	if len(contract.Invariants) > 0 {
		b.WriteString("| **Invariants** | ")
		for i, inv := range contract.Invariants {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(inv.ID)
		}
		b.WriteString(" |\n")
	}

	b.WriteString("\n")

	// Assessment if available
	if data.Summary.Assessment != nil {
		assessment := data.Summary.Assessment
		b.WriteString("### Assessment\n\n")

		status := "✅ Reached"
		if !assessment.Reached {
			status = "❌ Not Reached"
		}
		fmt.Fprintf(b, "**Status**: %s\n\n", status)
		fmt.Fprintf(b, "**Coverage**: %.1f%%\n\n", assessment.CoveragePct*100)

		if assessment.Reasoning != "" {
			fmt.Fprintf(b, "**Reasoning**: %s\n\n", assessment.Reasoning)
		}

		if len(assessment.Gaps) > 0 {
			b.WriteString("**Gaps**:\n\n")
			for _, gap := range assessment.Gaps {
				fmt.Fprintf(b, "- **%s**: %s\n", gap.Kind, gap.Detail)
			}
			b.WriteString("\n")
		}
	}
}

// joinStrings joins a slice of strings with a separator
func joinStrings(items []string, sep string) string {
	if len(items) == 0 {
		return "—"
	}
	return strings.Join(items, sep)
}
