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

// buildReflectionContext formats all step results for the reflection prompt.
// It creates a structured summary including test cases, status, HTTP responses,
// and evidence for AI reflection generation.
func (l *Learner) buildReflectionContext(results []agent.StepResult) string {
	var b strings.Builder
	b.WriteString("Test Results:\n\n")

	for i, r := range results {
		l.writeStepResult(&b, i, r)
		b.WriteString("\n")
	}

	return b.String()
}

// writeStepResult writes a single step result to the builder.
func (l *Learner) writeStepResult(b *strings.Builder, index int, r agent.StepResult) {
	fmt.Fprintf(b, "### Result %d: %s (%s)\n", index+1, r.TestCase.Name, r.TestCase.ID)
	fmt.Fprintf(b, "Status: %s\n", r.Status)
	fmt.Fprintf(b, "Target: %s\n", r.TestCase.Target)
	fmt.Fprintf(b, "Expectation: %s\n", r.TestCase.Expectation)
	fmt.Fprintf(b, "Attempts: %d\n", r.Attempts)

	l.writeResultDetails(b, r)
	l.writeErrorDetails(b, r)
	l.writeEvidenceDetails(b, r)
}

// writeResultDetails writes the result details depending on the result type.
func (l *Learner) writeResultDetails(b *strings.Builder, r agent.StepResult) {
	if r.Result == nil {
		return
	}

	if httpRes, ok := r.Result.(types.HTTPResult); ok {
		l.writeHTTPResult(b, httpRes)
	} else {
		fmt.Fprintf(b, "Result: %s\n", r.Result.Summary())
	}
}

// writeHTTPResult writes HTTP-specific result details.
func (l *Learner) writeHTTPResult(b *strings.Builder, res types.HTTPResult) {
	if res.StatusCode != 0 {
		fmt.Fprintf(b, "HTTP Status: %d\n", res.StatusCode)
	}

	if res.Body != "" {
		body := res.Body
		if len(body) > MaxResponseBodyLength {
			body = body[:MaxResponseBodyLength] + "..."
		}
		fmt.Fprintf(b, "Response: %s\n", body)
	}

	if res.Err != "" {
		fmt.Fprintf(b, "Error: %s\n", res.Err)
	}
}

// writeErrorDetails writes step error information if present.
func (l *Learner) writeErrorDetails(b *strings.Builder, r agent.StepResult) {
	if r.Error != nil {
		fmt.Fprintf(b, "Step Error: %s\n", r.Error)
	}
}

// writeEvidenceDetails writes evidence information if present.
func (l *Learner) writeEvidenceDetails(b *strings.Builder, r agent.StepResult) {
	if len(r.Evidence) == 0 {
		return
	}

	evJSON, _ := json.Marshal(r.Evidence)
	fmt.Fprintf(b, "Evidence: %s\n", string(evJSON))
}

// storeSemanticFromReflections extracts key facts from reflections and stores
// them as L2 semantic memory with embeddings for future retrieval.
// Only reflections that pass the quality gate are stored.
func (l *Learner) storeSemanticFromReflections(ctx context.Context, reflections []Reflection, project string) {
	for _, r := range reflections {
		if !qualityGate(r) {
			continue
		}

		if err := l.storeSingleReflection(ctx, r, project); err != nil {
			l.logger.Warn("store semantic memory", zap.Error(err))
		}
	}
}

// storeSingleReflection stores a single reflection as semantic memory.
func (l *Learner) storeSingleReflection(ctx context.Context, r Reflection, project string) error {
	content := fmt.Sprintf("%s: %s → %s", r.Type, r.Diagnosis, r.Strategy)
	embedding, err := l.embedder.Embed(ctx, content)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	_, err = l.store.StoreSemantic(ctx, content, "reflexion", project,
		[]string{r.Category, r.Type}, embedding, l.embedder.ModelName())
	return err
}
