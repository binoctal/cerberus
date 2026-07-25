package scout

import (
	"fmt"
	"math"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// bestToPlan converts the best candidate to a TestPlan.
func (t *ToTPlanner) bestToPlan(candidates []PlanCandidate, goal, baseURL string) *agent.TestPlan {
	if len(candidates) == 0 {
		return &agent.TestPlan{Goal: goal, ProjectURL: baseURL}
	}

	best := candidates[0]
	cases := make([]agent.TestCase, 0, len(best.Cases))
	for i, desc := range best.Cases {
		caseID := fmt.Sprintf("tot-%03d", i+1)
		priority := 1.0 - float64(i)*0.05
		if priority < 0 {
			priority = 0 // never collide with the isDeprioritized (<0) sentinel
		}
		cases = append(cases, agent.TestCase{
			ID:          caseID,
			Name:        truncateName(desc, 80),
			Target:      inferTarget(desc),
			Expectation: desc,
			Priority:    priority, // Slightly decreasing priority, floored at 0.
		})
	}

	return &agent.TestPlan{
		Goal:       goal,
		Cases:      cases,
		ProjectURL: baseURL,
	}
}

// formatModelForToT creates a compact model summary for ToT prompts.
func formatModelForToT(model *project.ProjectModel) string {
	var b strings.Builder
	if len(model.API.Endpoints) > 0 {
		b.WriteString("Endpoints:\n")
		for _, ep := range model.API.Endpoints {
			fmt.Fprintf(&b, "- %s %s (conf: %.1f)\n", ep.Method, ep.Path, ep.Confidence)
		}
	}
	if len(model.Navigation.Pages) > 0 {
		b.WriteString("Pages:\n")
		for _, pg := range model.Navigation.Pages {
			fmt.Fprintf(&b, "- %s (conf: %.1f)\n", pg.Path, pg.Confidence)
		}
	}
	if len(model.InvariantHints) > 0 {
		b.WriteString("Invariants:\n")
		for _, inv := range model.InvariantHints {
			fmt.Fprintf(&b, "- [%s] %s\n", inv.ID, inv.Description)
		}
	}
	if len(model.TechStack) > 0 {
		fmt.Fprintf(&b, "Tech stack: %s\n", strings.Join(model.TechStack, ", "))
	}
	return b.String()
}

// invariantCoverage returns the fraction of model.InvariantHints referenced
// by the candidate's cases (matched by ID or Description, case-insensitive).
// With no invariants modeled, returns 0.5 (neutral — neither rewarded nor
// penalized) so the invariant signal does not dominate when absent.
func (t *ToTPlanner) invariantCoverage(c *PlanCandidate, model *project.ProjectModel) float64 {
	if len(model.InvariantHints) == 0 {
		return 0.5
	}
	text := strings.ToLower(strings.Join(c.Cases, " "))
	matched := 0
	for _, inv := range model.InvariantHints {
		if strings.Contains(text, strings.ToLower(inv.ID)) || strings.Contains(text, strings.ToLower(inv.Description)) {
			matched++
		}
	}
	return float64(matched) / float64(len(model.InvariantHints))
}

// pageCoverage returns the fraction of model.Navigation.Pages referenced by
// the candidate's cases (matched by Path). Returns 0.5 when no pages are
// modeled (neutral default, same rationale as invariantCoverage).
func (t *ToTPlanner) pageCoverage(c *PlanCandidate, model *project.ProjectModel) float64 {
	if len(model.Navigation.Pages) == 0 {
		return 0.5
	}
	text := strings.ToLower(strings.Join(c.Cases, " "))
	matched := 0
	for _, pg := range model.Navigation.Pages {
		if strings.Contains(text, strings.ToLower(pg.Path)) {
			matched++
		}
	}
	return float64(matched) / float64(len(model.Navigation.Pages))
}

// actionDiversity rewards cases that span distinct test angles. It scans each
// case for keywords (get/post/error/edge/boundary/invariant/ws) and returns
// min(distinct_count/4, 1.0) — saturating at 4 distinct angles.
func (t *ToTPlanner) actionDiversity(c *PlanCandidate) float64 {
	set := map[string]bool{}
	for _, cs := range c.Cases {
		l := strings.ToLower(cs)
		for _, k := range []string{"get", "post", "error", "edge", "boundary", "invariant", "ws"} {
			if strings.Contains(l, k) {
				set[k] = true
			}
		}
	}
	return math.Min(float64(len(set))/4.0, 1.0) // saturate at 4 distinct angles
}

// goalOverlap returns the fraction of goal tokens (length > 2) that appear in
// the candidate's cases. Empty/whitespace goal yields 0.5 (neutral).
func (t *ToTPlanner) goalOverlap(c *PlanCandidate, goal string) float64 {
	if goal == "" {
		return 0.5
	}
	text := strings.ToLower(strings.Join(c.Cases, " "))
	toks := strings.Fields(strings.ToLower(goal))
	if len(toks) == 0 {
		return 0.5
	}
	matched := 0
	for _, tk := range toks {
		if len(tk) > 2 && strings.Contains(text, tk) {
			matched++
		}
	}
	return float64(matched) / float64(len(toks))
}

// truncateName truncates a string to maxLen, adding "..." if needed.
func truncateName(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// inferTarget attempts to extract an HTTP path from a test case description.
func inferTarget(desc string) string {
	desc = strings.ToLower(desc)
	// Look for patterns like "get /path" or "post /path".
	for _, method := range []string{"get", "post", "put", "delete", "patch"} {
		idx := strings.Index(desc, method+" /")
		if idx >= 0 {
			// Extract from the method to end of path-like segment.
			rest := desc[idx:]
			end := strings.IndexAny(rest, " \t\n,.")
			if end < 0 {
				end = len(rest)
			}
			return rest[:end]
		}
	}
	// Look for bare paths.
	if idx := strings.Index(desc, "/api/"); idx >= 0 {
		rest := desc[idx:]
		end := strings.IndexAny(rest, " \t\n,.")
		if end < 0 {
			end = len(rest)
		}
		return rest[:end]
	}
	return desc
}
