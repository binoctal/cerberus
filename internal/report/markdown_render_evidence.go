package report

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/store"
)

// renderEvidenceSection renders the evidence section
func renderEvidenceSection(b *strings.Builder, data *ReportData) {
	if len(data.Evidence) == 0 {
		return
	}

	b.WriteString("## Evidence\n\n")

	// Show evidence for verdicts
	shownTraces := make(map[int64]bool)
	for _, v := range data.Verdicts {
		evs, ok := data.Evidence[v.TraceID]
		if !ok || len(evs) == 0 {
			continue
		}
		fmt.Fprintf(b, "<details>\n<summary>%s (%d evidence)</summary>\n\n", v.Target, len(evs))
		renderEvidenceItems(b, evs)
		b.WriteString("</details>\n\n")
		shownTraces[v.TraceID] = true
	}

	// Show evidence for traces without verdicts
	for _, tr := range data.Traces {
		if shownTraces[tr.ID] {
			continue
		}
		evs, ok := data.Evidence[tr.ID]
		if !ok || len(evs) == 0 {
			continue
		}
		fmt.Fprintf(b, "<details>\n<summary>%s (%d evidence)</summary>\n\n", tr.Target, len(evs))
		renderEvidenceItems(b, evs)
		b.WriteString("</details>\n\n")
	}
}

// renderEvidenceItems renders individual evidence items
func renderEvidenceItems(b *strings.Builder, evs []store.Evidence) {
	for _, ev := range evs {
		fmt.Fprintf(b, "**[%s]** %s\n\n", ev.Type, truncate(ev.Content, 500))
	}
}
