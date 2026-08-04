package scout

import (
	"fmt"
	"sort"
	"strings"

	"github.com/binoctal/cerberus/internal/project"
)

// renderVocabSummary produces a compact, direction-grouped routing summary of
// every service's WS vocabulary for the planning prompt. It is prompt-only
// context: the LLM uses concrete type names to author ws_send/ws_receive
// choreography. Partial / unsupported / non-message_handled edges are counted
// in a footer rather than listed, so nothing is silently dropped. Returns ""
// when no service declares a vocabulary (byte-identical prompt for non-WS
// projects).
func renderVocabSummary(services []project.Service) string {
	var b strings.Builder
	for _, svc := range services {
		if svc.Vocabulary == nil || len(svc.Vocabulary.Edges) == 0 {
			continue
		}
		total := len(svc.Vocabulary.Edges)
		fmt.Fprintf(&b, "\n\n## WS Routing Vocabulary (%s, %d edges)\n", svc.Name, total)

		type group struct {
			label string
			from  string
			to    string
			mode  string
		}
		order := []group{}
		types := map[string][]string{}
		seen := map[string]map[string]bool{}
		skipped := 0

		for _, e := range svc.Vocabulary.Edges {
			if e.Partial || e.Unsupported || e.Trigger != "message_handled" {
				skipped++
				continue
			}
			label := e.Delivery.Mode
			if e.Delivery.ExcludeSender {
				label += "(exclude_sender)"
			}
			if e.RouteField != "" {
				label += fmt.Sprintf("[route=%s]", e.RouteField)
			}
			key := fmt.Sprintf("%s->%s %s", e.FromRole, e.ToRole, label)
			if _, ok := types[key]; !ok {
				order = append(order, group{label: key, from: e.FromRole, to: e.ToRole, mode: e.Delivery.Mode})
				types[key] = []string{}
				seen[key] = map[string]bool{}
			}
			if !seen[key][e.Type] {
				seen[key][e.Type] = true
				types[key] = append(types[key], e.Type)
			}
		}
		sort.Slice(order, func(i, j int) bool {
			if order[i].from != order[j].from {
				return order[i].from < order[j].from
			}
			if order[i].to != order[j].to {
				return order[i].to < order[j].to
			}
			return order[i].label < order[j].label
		})
		for _, g := range order {
			ts := types[g.label]
			fmt.Fprintf(&b, "%s (%d): %s\n", g.label, len(ts), strings.Join(ts, ", "))
		}
		if skipped > 0 {
			fmt.Fprintf(&b, "[skipped: %d partial/unsupported/non-message_handled edges]\n", skipped)
		}
	}
	return b.String()
}
