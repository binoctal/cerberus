package scout

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// vocabTypeSet returns the set of distinct message types declared by a
// vocabulary's edges. Used as the ground-truth type set for classifying
// plan tokens as hit vs invented. Nil-safe (returns empty set).
func vocabTypeSet(v *project.Vocabulary) map[string]bool {
	set := make(map[string]bool)
	if v == nil {
		return set
	}
	for _, e := range v.Edges {
		set[e.Type] = true
	}
	return set
}

// dumpPlan renders a TestPlan as indented JSON so the type-token extractor
// can scan a stable, complete representation (covers ws_send Message payloads
// and ws_receive Type fields alike).
func dumpPlan(plan *agent.TestPlan) string {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		// TestPlan is JSON-safe by construction; fall back to its goal only.
		return plan.Goal
	}
	return string(b)
}

// typeTokenRE matches namespace:action message types like "session:start",
// "device:online", "workflow:task_progress", "session:output-batch". The
// action half allows digits, underscores, and hyphens.
var typeTokenRE = regexp.MustCompile(`[a-z][a-z0-9_]*:[a-z][a-z0-9_-]*`)

// extractTypeTokens returns the distinct namespace:action tokens found in
// text, sorted and de-duplicated. Scanning the full JSON dump captures both
// ws_send payloads and ws_receive type fields.
func extractTypeTokens(text string) []string {
	matches := typeTokenRE.FindAllString(text, -1)
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

// classifyTypes splits tokens into hits (present in the vocabulary set) and
// invented (absent — model-fabricated types).
func classifyTypes(tokens []string, set map[string]bool) (hits, invented []string) {
	for _, tk := range tokens {
		if set[tk] {
			hits = append(hits, tk)
		} else {
			invented = append(invented, tk)
		}
	}
	return hits, invented
}

// scanFields returns the concatenation of a plan-dump's target, expectation,
// and steps fields (as decoded from JSON), excluding the truncated name field
// whose ~60-char tail previously produced false "invented" tokens. Used by the
// manual validation test so invented-list reflects true model fabrication.
func scanFields(dumpJSON string) string {
	var plan map[string]any
	if err := json.Unmarshal([]byte(dumpJSON), &plan); err != nil {
		return dumpJSON
	}
	var cases []any
	if c, ok := plan["cases"].([]any); ok {
		cases = c
	}
	var b strings.Builder
	for _, c := range cases {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"target", "expectation"} {
			if s, ok := m[key].(string); ok {
				b.WriteString(s)
				b.WriteByte('\n')
			}
		}
		if s, ok := m["steps"].([]any); ok {
			for _, st := range s {
				if sm, ok := st.(map[string]any); ok {
					if jb, err := json.Marshal(sm); err == nil {
						b.Write(jb)
						b.WriteByte('\n')
					}
				}
			}
		}
	}
	return b.String()
}

func TestScanFieldsExcludesNameField(t *testing.T) {
	// A truncated name whose tail looks like a namespace token must NOT be
	// scanned; a real type in the expectation MUST be.
	dump := `{"cases":[
		{"name":"web sends workflow:task_gu…","target":"ws://x/ws","expectation":"relay workflow:task_guidance"}
	]}`
	got := scanFields(dump)
	tokens := extractTypeTokens(got)
	for _, tk := range tokens {
		if tk == "workflow:task_gu" {
			t.Fatalf("truncated name tail leaked into scan: %v", tokens)
		}
	}
	hit := false
	for _, tk := range tokens {
		if tk == "workflow:task_guidance" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("expected workflow:task_guidance from expectation, got %v", tokens)
	}
}
