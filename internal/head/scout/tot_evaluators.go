package scout

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/binoctal/cerberus/internal/project"
)

// evaluate scores candidates deterministically (no LLM). Signals: endpoint
// coverage, invariant coverage, page coverage, action diversity, goal overlap.
// Fail-safe: if the top score is below floorScore, returns an error instead of
// silently ranking near-random candidates (the analogue of the old "all AI
// scores failed" systemic signal).
//
// goal is consumed by the goal-overlap signal; it is already in Plan's scope.
func (t *ToTPlanner) evaluate(ctx context.Context, candidates []PlanCandidate, model *project.ProjectModel, goal string) ([]PlanCandidate, error) {
	// ctx retained in signature for S3 trace/plumbing; evaluate is currently synchronous and LLM-free.

	for i := range candidates {
		c := candidates[i]
		c.Score = t.deterministicScore(&c, model, goal)
		c.Coverage = t.coverageScore(&c, model)
		c.AIScore = 0 // legacy field; deterministic replacement
		candidates[i] = c
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	if len(candidates) > 0 && candidates[0].Score < floorScore {
		return candidates, fmt.Errorf("tot evaluate: top score %.3f below floor %.3f; nothing actionable", candidates[0].Score, floorScore)
	}
	return candidates, nil
}

// floorScore is the minimum deterministic score for a candidate to be
// considered actionable. Below this the ranking is treated as noise and
// evaluate surfaces an error instead of returning near-random orderings.
const floorScore = 0.10

// deterministicScore combines five deterministic signals into a 0..1 score:
//
//	0.30 endpoint coverage   — how many known API endpoints the cases touch
//	0.25 invariant coverage  — how many invariant hints the cases reference
//	0.12 page coverage       — how many navigation pages the cases touch
//	0.20 action diversity    — breadth of test angles (get/post/error/edge/...)
//	0.13 goal overlap        — token overlap with the user's stated goal
//
// The weights sum to 1.0 and replace the former 70/30 LLM/coverage blend.
// ToT.propose stays on Decide (deferred to S3); evaluate is now LLM-free.
func (t *ToTPlanner) deterministicScore(c *PlanCandidate, model *project.ProjectModel, goal string) float64 {
	ep := t.coverageScore(c, model)      // 0.30
	inv := t.invariantCoverage(c, model) // 0.25
	pg := t.pageCoverage(c, model)       // 0.12
	div := t.actionDiversity(c)          // 0.20
	goalOL := t.goalOverlap(c, goal)     // 0.13
	return 0.30*ep + 0.25*inv + 0.12*pg + 0.20*div + 0.13*goalOL
}

// coverageScore computes a deterministic endpoint coverage score [0, 1].
func (t *ToTPlanner) coverageScore(c *PlanCandidate, model *project.ProjectModel) float64 {
	if len(model.API.Endpoints) == 0 {
		return 0.5 // No endpoints to cover.
	}

	// Count how many endpoint paths are mentioned in the strategy cases.
	casesText := strings.ToLower(strings.Join(c.Cases, " "))
	matched := 0
	for _, ep := range model.API.Endpoints {
		pathKey := strings.ToLower(strings.TrimPrefix(ep.Path, "/"))
		if pathKey == "" {
			continue
		}
		// Check if any part of the path appears in the cases text.
		parts := strings.Split(pathKey, "/")
		for _, part := range parts {
			if len(part) > 2 && strings.Contains(casesText, part) {
				matched++
				break
			}
		}
	}

	return float64(matched) / float64(len(model.API.Endpoints))
}
