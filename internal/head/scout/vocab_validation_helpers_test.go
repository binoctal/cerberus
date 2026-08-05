package scout

import (
	"encoding/json"

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
