package examiner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
)

// dimensionGuidance is prepended to the evidence context only when the result
// carries at least one dimension. It is a SOFT instruction — whether the LLM
// honors it is itself the thing the validation measures, not a guarantee.
//
// The critical distinction is between a DIMENSION the claim references that has
// no evidence (a real gap ⇒ uncertain) and a dimension that IS present but
// whose sub-fact is "not measured" (an active probe did not run). The latter is
// not a gap unless the claim specifically requires that sub-fact: a membership
// dimension listing recipients satisfies a "reaches both peers" claim even when
// sender-exclusion is unmeasured, because the claim says nothing about the
// sender. Without this scoping the judge over-triggers on "not measured" and
// drifts on recipient-only claims (measured 2026-08-06, fanout case).
const dimensionGuidance = "The evidence below is organized by dimension: count, membership, ordering, value, presence. Map each claim in the expectation to its matching dimension and check the typed fact. A dimension is satisfied when its measured facts meet the claim. Some sub-facts are marked \"not measured\" (no active probe ran for them): treat that as a neutral scope note, NOT a gap — only return uncertain when the claim specifically requires a sub-fact that is marked \"not measured\". An unmeasured sub-fact the claim does not reference must not lower your confidence, and you must never infer its outcome from the other fields. Membership recipients only list connections the test itself observed: an empty recipients list on a sent type means no test connection watched for it, NOT that delivery failed — when the claim is delivery to an external actor (e.g. a real bridge), response frames in the raw evidence that reference the sent message (same type family, task/session/device ids) are valid proof of receipt."

// recipientsNotProbedNote marks a membership dimension whose type was sent
// but never observed back on any test connection. Without it the rendered
// bare recipients=[] reads as a measured negative and sinks verdicts whose
// delivery proof lives in the raw evidence (dogfood run 10,
// ws-realtime-wf-task-assign).
const recipientsNotProbedNote = "no test connection observed this type; delivery to external actors is not directly probed — response frames referencing the sent message are valid proof of receipt"

// dimensionsFor returns the merged dimension set for a step result: source 1
// (result-carried, Evidence().Dimensions) and source 2 (flow-derived,
// deriveDimensions). Source-1 wins on (Kind, Label) conflict.
func (j *Judge) dimensionsFor(r agent.StepResult) []types.Dimension {
	var s1 []types.Dimension
	if r.Result != nil {
		s1 = r.Result.Evidence().Dimensions
	}
	var s2 []types.Dimension
	if j.deriveEnabled {
		s2 = j.deriveDimensions(r)
	}
	return mergeDimensions(s1, s2)
}

// mergeDimensions concatenates two dimension sets, de-duplicating by
// (Kind, Label) with the first occurrence (source 1) winning.
func mergeDimensions(s1, s2 []types.Dimension) []types.Dimension {
	if len(s1) == 0 && len(s2) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]types.Dimension, 0, len(s1)+len(s2))
	for _, d := range s1 {
		k := d.Kind + "|" + d.Label
		if !seen[k] {
			seen[k] = true
			out = append(out, d)
		}
	}
	for _, d := range s2 {
		k := d.Kind + "|" + d.Label
		if !seen[k] {
			seen[k] = true
			out = append(out, d)
		}
	}
	return out
}

// renderDimensions formats a dimension set as a prompt block. Returns "" when
// empty, so callers emit nothing and the prompt is byte-identical to the
// no-dimension path (zero regression).
func renderDimensions(dims []types.Dimension) string {
	if len(dims) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Structured Evidence (by dimension):\n")
	for _, d := range dims {
		line := ""
		switch d.Kind {
		case "membership":
			line = fmt.Sprintf("recipients=%v", d.Recipients)
			if d.Sender != "" {
				line += fmt.Sprintf("; sender=%s", d.Sender)
			}
			if d.Excluded == nil {
				line += "; sender-exclusion not measured"
			} else if *d.Excluded {
				line += "; sender excluded"
			} else {
				line += "; sender NOT excluded"
			}
		case "count":
			line = fmt.Sprintf("count=%d", d.Count)
		case "value":
			line = d.Value
		case "presence":
			if d.Present != nil {
				line = fmt.Sprintf("present=%v", *d.Present)
			} else {
				line = "present unknown"
			}
		case "ordering":
			line = fmt.Sprintf("order=%v", d.Order)
		}
		if d.Note != "" {
			line += " (" + d.Note + ")"
		}
		fmt.Fprintf(&b, "  [%s] %s: %s\n", d.Kind, d.Label, line)
	}
	return b.String()
}

// deriveDimensions produces flow-level dimensions (source 2) from a step
// result's per-step trace.
//
// membership: for each message type that a ws_send sent, the recipients are
// the connections whose ws_receive matched it, and the sender is the ws_send
// connection. Excluded is set from a sender negative-probe (ExpectAbsent) when
// one ran for that type: no echo ⇒ *true (sender excluded), echo ⇒ *false
// (sender received its own broadcast). Pass 1 establishes senders +
// recipients; pass 2 resolves probes against the now-known sender so Evidence
// ordering does not matter. Without a probe, Excluded stays nil — the honest
// "sender-exclusion not measured" state the judge already renders.
//
// count: per (type, connection) — the total frames that connection positively
// observed for the type, accumulated across receive steps. The observed count
// only is reported; comparison against the claim (no-loss/no-dup) belongs to
// the judge.
//
// ordering: per (type, connection) when the connection observed the type at
// least twice — the concatenated order-key list (evidence order is temporal
// order; MatchedOrder within a burst is arrival order). Derived WITHOUT any
// ws_send requirement so server-pushed batched types (e.g. session:output)
// still yield facts. Placeholder (#i) lists carry a "positional" Note.
func (j *Judge) deriveDimensions(r agent.StepResult) []types.Dimension {
	senders := map[string]string{}             // type -> sender connectionID
	recipients := map[string]map[string]bool{} // type -> set of recipient connectionIDs
	counts := map[string]int{}                 // type|conn -> observed frames
	orders := map[string][]string{}            // type|conn -> ordered keys
	var probes []agent.Evidence                // ExpectAbsent receives, resolved in pass 2
	for _, ev := range r.Evidence {
		if ev.MatchedType == "" {
			continue
		}
		switch ev.Action {
		case "ws_send":
			if _, ok := senders[ev.MatchedType]; !ok {
				senders[ev.MatchedType] = ev.ConnectionID
			}
			if recipients[ev.MatchedType] == nil {
				recipients[ev.MatchedType] = map[string]bool{}
			}
		case "ws_receive":
			if ev.ExpectAbsent {
				probes = append(probes, ev)
				continue
			}
			if ev.Matched {
				if recipients[ev.MatchedType] == nil {
					recipients[ev.MatchedType] = map[string]bool{}
				}
				recipients[ev.MatchedType][ev.ConnectionID] = true
				k := ev.MatchedType + "|" + ev.ConnectionID
				n := ev.MatchedCount
				if n == 0 {
					n = 1
				}
				counts[k] += n
				orders[k] = append(orders[k], ev.MatchedOrder...)
			}
		}
	}
	// Pass 2: a probe settles Excluded only for its type's sender connection.
	excluded := map[string]*bool{}
	for _, p := range probes {
		sender, ok := senders[p.MatchedType]
		if !ok || p.ConnectionID != sender {
			continue
		}
		b := !p.Matched // no echo ⇒ sender excluded
		excluded[p.MatchedType] = &b
	}
	out := make([]types.Dimension, 0, len(senders)+len(counts))
	typesSorted := make([]string, 0, len(senders))
	for t := range senders {
		typesSorted = append(typesSorted, t)
	}
	sort.Strings(typesSorted)
	for _, t := range typesSorted {
		rcv := make([]string, 0, len(recipients[t]))
		for c := range recipients[t] {
			rcv = append(rcv, c)
		}
		sort.Strings(rcv)
		dim := types.Dimension{
			Kind:       "membership",
			Label:      t + " recipients",
			Recipients: rcv,
			Sender:     senders[t],
		}
		if len(rcv) == 0 {
			dim.Note = recipientsNotProbedNote
		}
		if e, ok := excluded[t]; ok {
			dim.Excluded = e
		}
		out = append(out, dim)
	}
	pairsSorted := make([]string, 0, len(counts))
	for k := range counts {
		pairsSorted = append(pairsSorted, k)
	}
	sort.Strings(pairsSorted)
	for _, k := range pairsSorted {
		parts := strings.SplitN(k, "|", 2)
		out = append(out, types.Dimension{
			Kind:  "count",
			Label: parts[0] + " on " + parts[1],
			Count: counts[k],
		})
		if len(orders[k]) >= 2 {
			dim := types.Dimension{
				Kind:  "ordering",
				Label: parts[0] + " order on " + parts[1],
				Order: orders[k],
			}
			if strings.HasPrefix(orders[k][0], "#") {
				dim.Note = "positional order; frames carry no comparable ids"
			}
			out = append(out, dim)
		}
	}
	return out
}
