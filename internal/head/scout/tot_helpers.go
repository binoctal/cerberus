package scout

import (
	"fmt"
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

// formatEndpointsForEval creates a compact endpoint list for evaluation prompts.
func formatEndpointsForEval(model *project.ProjectModel) string {
	if len(model.API.Endpoints) == 0 {
		return "(no known endpoints)"
	}
	var parts []string
	for _, ep := range model.API.Endpoints {
		parts = append(parts, ep.Method+" "+ep.Path)
	}
	return strings.Join(parts, ", ")
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
