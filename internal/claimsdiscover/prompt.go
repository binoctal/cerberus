package claimsdiscover

import (
	"fmt"
	"sort"
	"strings"

	"github.com/binoctal/cerberus/internal/llm"
)

// buildExtractPrompt assembles the extraction prompt from the doc sources.
// Docs are joined in sorted-path order (deterministic prompt for a given
// input) with numbered lines so the model can cite file:line in source_ref.
// The prompt states the contract; the claims_extract tool schema carries the
// JSON shape.
func buildExtractPrompt(docs map[string]string, max int) string {
	var b strings.Builder
	b.WriteString("You are extracting the claims ledger for a cerberus test project.\n")
	b.WriteString("Read the product docs below and call the claims_extract tool once with every falsifiable capability claim the product makes about itself.\n\n")
	fmt.Fprintf(&b, "Contract:\n- ONLY falsifiable capability claims — a claim must be checkable by executing the system.\n- REJECT marketing adjectives and unfalsifiable prose (\"delightful\", \"blazing fast\", \"best-in-class\"); do not convert them into claims.\n- At most %d claims; pick the most significant.\n", max)
	b.WriteString("- Each claim: id (stable kebab-case, e.g. multi-bridge-pairing), text (the falsifiable claim, verbatim intent not marketing), source_ref (file:line when identifiable, else file), implies_cardinality (2+ only when the text implies multiple instances/agents/devices, e.g. \"multiple bridges\"; else omit).\n\n")
	paths := make([]string, 0, len(docs))
	for p := range docs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		b.WriteString("--- " + p + " ---\n")
		b.WriteString(numberLines(truncateDoc(docs[p])))
		b.WriteString("\n\n")
	}
	return b.String()
}

// numberLines prefixes each line with its 1-based number so source_ref can
// cite file:line accurately.
func numberLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = fmt.Sprintf("%d: %s", i+1, l)
	}
	return strings.Join(lines, "\n")
}

// truncateDoc caps one doc's contribution to the prompt (rune count so a cut
// never splits a multi-byte sequence — same rationale as protocoldiscover).
func truncateDoc(s string) string {
	const cap = 8000
	r := []rune(s)
	if len(r) <= cap {
		return s
	}
	return string(r[:cap]) + "\n…[truncated]"
}

// claimsExtractTool is the typed tool Extract offers the LLM. Hand-written
// schema (cerberus has no struct->schema reflection; mirrors
// protocoldiscover's protocolDraftTool).
func claimsExtractTool() llm.Tool {
	return llm.Tool{
		Name:        "claims_extract",
		Description: "Extract the product's falsifiable capability claims from the provided docs. Call once; claims may be empty when the docs make no capability claims.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"claims": map[string]any{
					"type":        "array",
					"maxItems":    DefaultMaxClaims,
					"description": "Falsifiable capability claims, most significant first.",
					"items": map[string]any{
						"type":     "object",
						"required": []any{"id", "text"},
						"properties": map[string]any{
							"id":                  map[string]any{"type": "string", "pattern": "^[a-z0-9]+(-[a-z0-9]+)*$", "description": "Stable kebab-case identifier."},
							"text":                map[string]any{"type": "string", "description": "The falsifiable claim."},
							"source_ref":          map[string]any{"type": "string", "description": "file:line when identifiable, else file."},
							"implies_cardinality": map[string]any{"type": "integer", "minimum": 2, "description": "Set only when the claim implies multiple instances/agents/devices."},
						},
					},
				},
			},
			"required": []any{"claims"},
		},
	}
}
