// Command protoinferbench runs `cerberus protocol infer` N times against a live
// target, scores each draft against a fixed ground truth per-structure, and
// reports per-structure hit rates with threshold-gated PASS/FAIL. It exists to
// kill the subjective "looks right over 3-5 runs" loop: variance dominates small
// samples, so we run >=15, score structures objectively, and judge by thresholds.
//
// The benchmark is env-gated: it only touches the network when CERBERUS_BENCH=1.
// All pure scoring logic lives here (no I/O) so `go test` is network-free.
package main

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/project"
)

// Scored structure indices, in fixed reporting order. Add to the bottom only;
// the order is part of the report contract.
const (
	idxFraming = iota
	idxTypePath
	idxAuth
	idxRoles
	idxHandshake
	idxBatchKeys
	idxBatchItemsPath
	numStructures
)

// structureNames maps each index to its report label.
var structureNames = [numStructures]string{
	"framing", "type_path", "auth", "roles", "handshake",
	"batch_keys", "batch_items_path",
}

// thresholds are the minimum hit rate (0..1) for each structure to PASS.
var thresholds = [numStructures]float64{
	0.95, 0.95, 0.95, 0.90, 0.60, 0.50, 0.40,
}

// Ground-truth literals for the open-agents target.
const (
	gtFraming        = "json"
	gtTypePath       = "type"
	gtAuthStrategy   = "query"
	gtAuthParam      = "token"
	gtRoleWeb        = "web"
	gtRoleBridge     = "bridge"
	gtParamTypeKey   = "type"
	gtWebParamType   = "web"
	gtBridgePType    = "bridge"
	gtHandshakeType  = "devices:sync"
	gtBatchKey       = "session:output-batch"
	gtBatchItemType  = "session:output"
	gtBatchItemsPath = "payload.lines"
)

// runOutcome classifies what a single `protocol infer` invocation produced.
type runOutcome string

const (
	outcomeDraft      runOutcome = "draft"
	outcomeNoProtocol runOutcome = "no_protocol"
	outcomeParseFail  runOutcome = "parse_fail"
	outcomeHardError  runOutcome = "hard_error"
)

// runResult is the outcome of one benchmark run plus its parsed draft (nil for
// non-draft outcomes).
type runResult struct {
	outcome runOutcome
	proto   *project.Protocol
}

// hasProto reports whether the run yielded a usable draft.
func (r runResult) hasProto() bool { return r.proto != nil }

// draftPrefix is the stdout marker emitted by `protocol infer --dry-run`.
const draftPrefix = "Draft protocol "

// errNoDraft is returned by ParseDraft when stdout carries no draft marker.
var errNoDraft = errors.New("no draft in output")

// ParseDraft extracts the YAML block following the "Draft protocol" line and
// unmarshals it into a *project.Protocol. It returns errNoDraft when the marker
// is absent (e.g. the "no WebSocket protocol found" message).
func ParseDraft(stdout string) (*project.Protocol, error) {
	idx := strings.Index(stdout, draftPrefix)
	if idx < 0 {
		return nil, errNoDraft
	}
	// Drop the marker line; everything after it is the YAML body.
	body := stdout[idx:]
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	}
	var p project.Protocol
	if err := yaml.Unmarshal([]byte(body), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// classifyRun inspects subprocess output and maps it to a runResult. It never
// returns an error: every failure mode (no protocol, parse failure, non-zero
// exit) becomes a non-draft outcome so the run still counts in the denominator.
func classifyRun(stdout, _ string, exitCode int) runResult {
	if exitCode != 0 {
		return runResult{outcome: outcomeHardError}
	}
	if strings.Contains(stdout, "no WebSocket protocol found") {
		return runResult{outcome: outcomeNoProtocol}
	}
	p, err := ParseDraft(stdout)
	if err != nil {
		return runResult{outcome: outcomeParseFail}
	}
	return runResult{outcome: outcomeDraft, proto: p}
}

// Score evaluates one draft into a fixed-order hit vector. A nil proto (failed
// run) is an all-miss vector.
func Score(p *project.Protocol) [numStructures]bool {
	var h [numStructures]bool
	if p == nil {
		return h
	}
	h[idxFraming] = p.Framing == gtFraming
	h[idxTypePath] = p.TypePath == gtTypePath
	h[idxAuth] = p.Auth != nil && p.Auth.Strategy == gtAuthStrategy && p.Auth.Param == gtAuthParam

	// roles is hit only when BOTH the web and bridge roles are present, each
	// with its correct `type` discriminator param. Spec §3 row 4; both roles
	// are part of the ground truth (a partial set with only web does not count).
	web, hasWeb := p.Roles[gtRoleWeb]
	bridge, hasBridge := p.Roles[gtRoleBridge]
	h[idxRoles] = hasWeb && hasBridge &&
		web.Params[gtParamTypeKey] == gtWebParamType &&
		bridge.Params[gtParamTypeKey] == gtBridgePType

	h[idxHandshake] = hasWeb && web.Handshake != nil &&
		web.Handshake.AwaitType == gtHandshakeType && web.Handshake.Optional

	batch, hasBatch := p.Batches[gtBatchKey]
	h[idxBatchKeys] = hasBatch && batch.ItemType == gtBatchItemType
	h[idxBatchItemsPath] = hasBatch && batch.ItemsPath == gtBatchItemsPath
	return h
}

// structureStat is one row of the report.
type structureStat struct {
	name      string
	hits      int
	n         int
	threshold float64
}

// rate is the hit fraction in [0,1].
func (s structureStat) rate() float64 {
	if s.n == 0 {
		return 0
	}
	return float64(s.hits) / float64(s.n)
}

// report is the aggregated benchmark result.
type report struct {
	n           int
	samplesHint string
	structures  []structureStat
	outcomes    map[runOutcome]int
	overall     bool
}

// Aggregate tallies per-structure hits across results and applies thresholds.
// Failed runs (non-draft outcomes) contribute 0 hits but count in the
// denominator n, matching the brief's "pass-2 drift counts as not-hit".
func Aggregate(results []runResult, samplesHint string) report {
	rep := report{
		n:           len(results),
		samplesHint: samplesHint,
		structures:  make([]structureStat, numStructures),
		outcomes:    map[runOutcome]int{},
	}
	for i := 0; i < numStructures; i++ {
		rep.structures[i] = structureStat{
			name: structureNames[i], n: rep.n, threshold: thresholds[i],
		}
	}
	for _, r := range results {
		rep.outcomes[r.outcome]++
		h := Score(r.proto)
		for i := 0; i < numStructures; i++ {
			if h[i] {
				rep.structures[i].hits++
			}
		}
	}
	rep.overall = true
	for _, s := range rep.structures {
		if s.rate() < s.threshold {
			rep.overall = false
			break
		}
	}
	return rep
}

// formatReport renders the benchmark result as a markdown table pasteable into
// the dogfood doc.
func formatReport(rep report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Protocol infer benchmark — open-agents (N=%d, samples=%s per run)\n\n",
		rep.n, rep.samplesHint)
	fmt.Fprintf(&b, "Run outcomes:\n")
	// Stable order: draft, no_protocol, parse_fail, hard_error.
	for _, o := range []runOutcome{outcomeDraft, outcomeNoProtocol, outcomeParseFail, outcomeHardError} {
		fmt.Fprintf(&b, "  %-11s : %2d\n", o, rep.outcomes[o])
	}
	fmt.Fprintf(&b, "\nPer-structure hit rate:\n")
	fmt.Fprintf(&b, "| Structure        | Hits  | Rate  | Threshold | Result |\n")
	fmt.Fprintf(&b, "|------------------|-------|-------|-----------|--------|\n")
	passed := 0
	for _, s := range rep.structures {
		rate := s.rate()
		res := "FAIL"
		if rate >= s.threshold {
			res, passed = "PASS", passed+1
		}
		fmt.Fprintf(&b, "| %-16s | %d/%d | %3.0f%%  | >=%-2.0f%%     | %-6s |\n",
			s.name, s.hits, s.n, rate*100, s.threshold*100, res)
	}
	verdict := "FAIL"
	if rep.overall {
		verdict = "PASS"
	}
	fmt.Fprintf(&b, "\nOverall: %s (%d/7 structures)\n", verdict, passed)
	return b.String()
}
