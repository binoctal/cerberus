package examiner

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
)

// dimensionGuidance is prepended to the evidence context only when the result
// carries at least one dimension. It is a SOFT instruction — whether the LLM
// honors "missing dimension → uncertain" is itself the thing the validation
// measures, not a guarantee.
const dimensionGuidance = "The evidence below is organized by dimension: count, membership, ordering, value, presence. Map each claim in the expectation to its matching dimension and check the typed fact. If the expectation depends on a dimension for which no evidence is listed, return uncertain with low confidence — do not infer the outcome from unrelated evidence."

// dimensionsFor returns the merged dimension set for a step result: source 1
// (result-carried, Evidence().Dimensions) and source 2 (flow-derived,
// deriveDimensions). Source-1 wins on (Kind, Label) conflict.
func (j *Judge) dimensionsFor(r agent.StepResult) []types.Dimension {
	var s1 []types.Dimension
	if r.Result != nil {
		s1 = r.Result.Evidence().Dimensions
	}
	return mergeDimensions(s1, j.deriveDimensions(r))
}

// mergeDimensions concatenates two dimension sets, de-duplicating by
// (Kind, Label) with the first occurrence (source 1) winning.
func mergeDimensions(s1, s2 []types.Dimension) []types.Dimension {
	if len(s1) == 0 && len(s2) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]types.Dimension, 0, len(s1)+len(s2))
	for _, d := range s1 {
		k := d.Kind + "|" + d.Label
		if !seen[k] {
			seen[k] = true
			out = append(out, d)
		}
	}
	for _, d := range s2 {
		k := d.Kind + "|" + d.Label
		if !seen[k] {
			seen[k] = true
			out = append(out, d)
		}
	}
	return out
}

// renderDimensions formats a dimension set as a prompt block. Returns "" when
// empty, so callers emit nothing and the prompt is byte-identical to the
// no-dimension path (zero regression).
func renderDimensions(dims []types.Dimension) string {
	if len(dims) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Structured Evidence (by dimension):\n")
	for _, d := range dims {
		line := ""
		switch d.Kind {
		case "membership":
			line = fmt.Sprintf("recipients=%v", d.Recipients)
			if d.Sender != "" {
				line += fmt.Sprintf("; sender=%s", d.Sender)
			}
			if d.Excluded == nil {
				line += "; sender exclusion not probed"
			} else if *d.Excluded {
				line += "; sender excluded"
			} else {
				line += "; sender NOT excluded"
			}
		case "count":
			line = fmt.Sprintf("count=%d", d.Count)
		case "value":
			line = d.Value
		case "presence":
			if d.Present != nil {
				line = fmt.Sprintf("present=%v", *d.Present)
			} else {
				line = "present unknown"
			}
		case "ordering":
			line = fmt.Sprintf("order=%v", d.Order)
		}
		if d.Note != "" {
			line += " (" + d.Note + ")"
		}
		fmt.Fprintf(&b, "  [%s] %s: %s\n", d.Kind, d.Label, line)
	}
	return b.String()
}

// deriveDimensions produces flow-level dimensions (source 2) from a step
// result's per-step trace. Stub here (returns nil); the ws_flow membership
// derivation lands in a follow-up task.
func (j *Judge) deriveDimensions(r agent.StepResult) []types.Dimension {
	_ = r
	return nil
}
