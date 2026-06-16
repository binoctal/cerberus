package scout

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// extractUniqueTargets collects unique endpoint paths from the model.
func extractUniqueTargets(model *project.ProjectModel) []string {
	seen := make(map[string]bool)
	var targets []string
	for _, ep := range model.API.Endpoints {
		key := ep.Method + " " + ep.Path
		if !seen[key] {
			seen[key] = true
			targets = append(targets, ep.Path) // Use path for episodic lookup
		}
	}
	return targets
}

// queryEpisodicMemories retrieves past test results for each target.
func (s *Scout) queryEpisodicMemories(ctx context.Context, targets []string, limit int) []store.EpisodicMemory {
	var memories []store.EpisodicMemory

	for _, target := range targets {
		targetMemories, err := s.store.GetEpisodicByTarget(ctx, target, limit)
		if err != nil {
			s.logger.Debug("episodic lookup failed", zap.String("target", target), zap.Error(err))
			continue
		}
		memories = append(memories, targetMemories...)
	}

	return memories
}

// formatEpisodicMemories formats episodic memories into a readable summary.
func formatEpisodicMemories(targets []string, memories []store.EpisodicMemory) string {
	// Group memories by target
	memoryByTarget := make(map[string][]store.EpisodicMemory)
	for _, m := range memories {
		memoryByTarget[m.Target] = append(memoryByTarget[m.Target], m)
	}

	var b strings.Builder
	for _, target := range targets {
		targetMemories, ok := memoryByTarget[target]
		if !ok || len(targetMemories) == 0 {
			continue
		}

		fmt.Fprintf(&b, "Target %s:\n", target)
		for _, m := range targetMemories {
			fmt.Fprintf(&b, "- %s (verdict: %s, duration: %dms)\n", m.Status, m.Verdict, m.DurationMs)
		}
	}

	return b.String()
}

// querySemanticMemories searches for facts related to the goal using embeddings.
func (s *Scout) querySemanticMemories(ctx context.Context, goal string, topK int, threshold float64) ([]store.SemanticSearchResult, error) {
	queryEmb, err := s.embedder.Embed(ctx, goal)
	if err != nil {
		return nil, err
	}

	results, err := s.store.SearchSemanticForProject(ctx, queryEmb, s.config.Project.Name, topK, threshold)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// formatSemanticMemories formats semantic search results into a readable summary.
func formatSemanticMemories(memories []store.SemanticSearchResult) string {
	if len(memories) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\nRelated past insights:\n")
	for _, sr := range memories {
		fmt.Fprintf(&b, "- %s (score: %.2f)\n", sr.Content, sr.Score)
	}

	return b.String()
}

// buildEpisodicContext queries L1 episodic memory for known targets and formats
// a summary of previous test outcomes to inform planning.
func (s *Scout) buildEpisodicContext(ctx context.Context, goal string, model *project.ProjectModel) string {
	// Phase 1: Extract unique targets from the model
	targets := extractUniqueTargets(model)

	// Phase 2: Query episodic memories for all targets
	episodicMemories := s.queryEpisodicMemories(ctx, targets, s.reflexionCfg.EpisodicLimit)

	// Phase 3: Format episodic memories
	summary := formatEpisodicMemories(targets, episodicMemories)

	// Phase 4: Query and format semantic memories (L2)
	if goal != "" {
		semanticMemories, err := s.querySemanticMemories(ctx, goal, s.reflexionCfg.SemanticTopK, s.reflexionCfg.SemanticThreshold)
		if err != nil {
			s.logger.Debug("semantic search failed", zap.Error(err))
		} else {
			summary += formatSemanticMemories(semanticMemories)
		}
	}

	return summary
}
