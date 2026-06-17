package examiner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
)

// qualityGate validates a reflection before L3 storage.
func qualityGate(r Reflection) bool {
	if r.Diagnosis == "" {
		return false
	}
	if len(r.Strategy) < 10 {
		return false
	}
	if r.ConditionPattern == "" {
		return false
	}
	if r.Type != "failure" && r.Type != "success" {
		return false
	}
	return true
}

// buildReflectionContext formats all step results for the reflection prompt.
func (l *Learner) buildReflectionContext(results []agent.StepResult) string {
	var b strings.Builder
	b.WriteString("Test Results:\n\n")

	for i, r := range results {
		fmt.Fprintf(&b, "### Result %d: %s (%s)\n", i+1, r.TestCase.Name, r.TestCase.ID)
		fmt.Fprintf(&b, "Status: %s\n", r.Status)
		fmt.Fprintf(&b, "Target: %s\n", r.TestCase.Target)
		fmt.Fprintf(&b, "Expectation: %s\n", r.TestCase.Expectation)
		fmt.Fprintf(&b, "Attempts: %d\n", r.Attempts)

		if r.Result != nil {
			if httpRes, ok := r.Result.(types.HTTPResult); ok {
				if httpRes.StatusCode != 0 {
					fmt.Fprintf(&b, "HTTP Status: %d\n", httpRes.StatusCode)
				}
				if httpRes.Body != "" {
					body := httpRes.Body
					if len(body) > 500 {
						body = body[:500] + "..."
					}
					fmt.Fprintf(&b, "Response: %s\n", body)
				}
				if httpRes.Err != "" {
					fmt.Fprintf(&b, "Error: %s\n", httpRes.Err)
				}
			} else {
				fmt.Fprintf(&b, "Result: %s\n", r.Result.Summary())
			}
		}
		if r.Error != nil {
			fmt.Fprintf(&b, "Step Error: %s\n", r.Error)
		}

		// Include evidence if available.
		if len(r.Evidence) > 0 {
			evJSON, _ := json.Marshal(r.Evidence)
			fmt.Fprintf(&b, "Evidence: %s\n", string(evJSON))
		}

		b.WriteString("\n")
	}

	return b.String()
}

// storeSemanticFromReflections extracts key facts from reflections and stores
// them as L2 semantic memory with embeddings for future retrieval.
func (l *Learner) storeSemanticFromReflections(ctx context.Context, reflections []Reflection, project string) {
	for _, r := range reflections {
		if !qualityGate(r) {
			continue
		}
		content := fmt.Sprintf("%s: %s → %s", r.Type, r.Diagnosis, r.Strategy)
		embedding, err := l.embedder.Embed(ctx, content)
		if err != nil {
			l.logger.Warn("generate embedding", zap.Error(err))
			continue
		}

		_, storeErr := l.store.StoreSemantic(ctx, content, "reflexion", project,
			[]string{r.Category, r.Type}, embedding, l.embedder.ModelName())
		if storeErr != nil {
			l.logger.Warn("store semantic memory", zap.Error(storeErr))
		}
	}
}
