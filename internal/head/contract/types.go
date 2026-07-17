package contract

import (
	"encoding/json"
	"fmt"
)

// Contract is the AI-authored coverage standard for a session.
type Contract struct {
	Depth        string         `json:"depth"`         // smoke | standard | thorough
	Scope        []string       `json:"scope"`         // modules/paths to cover
	PathTypes    []string       `json:"path_types"`    // happy | alternative | boundary | edge
	ErrorScope   []string       `json:"error_scope"`   // 4xx | validation | exception
	Boundaries   []string       `json:"boundaries"`    // empty | zero | max | invalid | extreme
	Invariants   []InvariantRef `json:"invariants"`    // pulled from project.yaml invariants
	Priorities   Priorities     `json:"priorities"`    // priority bucket → modules (see Priorities)
	CoverageGate Gate           `json:"coverage_gate"` // objective coverage threshold
}

// Priorities maps a priority bucket to its modules. It tolerates the two
// shapes real LLMs emit so a non-Claude model returning {"check":"level"}
// (map[string]string) does not fail coverage-contract parsing:
//   - map[string][]string (documented): {"high": ["go/build"]}
//   - map[string]string (common):       {"health_check": "critical"}
//
// Single string values are normalized to one-element slices, so consumers
// always see []string regardless of which shape the model produced.
type Priorities map[string][]string

// UnmarshalJSON decodes Priorities from either supported shape.
func (p *Priorities) UnmarshalJSON(data []byte) error {
	out := make(Priorities)

	var multi map[string][]string
	if err := json.Unmarshal(data, &multi); err == nil {
		for k, v := range multi {
			out[k] = v
		}
		*p = out
		return nil
	}

	var single map[string]string
	if err := json.Unmarshal(data, &single); err == nil {
		for k, v := range single {
			out[k] = []string{v}
		}
		*p = out
		return nil
	}

	return fmt.Errorf("priorities must be {string:string} or {string:string[]}")
}

// InvariantRef references a project invariant the contract must enforce.
type InvariantRef struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// Gate is an objective coverage threshold (language-agnostic; each coverage
// provider measures, the contract only stores the threshold).
type Gate struct {
	Module          string  `json:"module"`
	LineThreshold   float64 `json:"line_threshold"`
	BranchThreshold float64 `json:"branch_threshold"`
}

// Assessment is the Examiner's session-level verdict against a Contract.
type Assessment struct {
	Reached     bool    `json:"reached"`      // contract satisfied?
	Gaps        []Gap   `json:"gaps"`         // what's missing
	CoveragePct float64 `json:"coverage_pct"` // objective coverage of gated module
	Reasoning   string  `json:"reasoning"`
}

// Gap describes a coverage shortfall found during assessment.
type Gap struct {
	Kind   string `json:"kind"` // scope | pathtype | error | boundary | invariant | coverage
	Detail string `json:"detail"`
}

// CoverageMeasurement is the objective coverage value passed to AssessCoverage.
// Pct is a 0–1 fraction (matching Gate.LineThreshold and Assessment.CoveragePct),
// NOT a 0–100 percentage. Unit is "line" (Go) or "function" (Node/Python).
// Known is false when no measurement could be obtained (provider failure or
// nothing measurable); a measured 0% has Known=true.
type CoverageMeasurement struct {
	Pct   float64
	Unit  string
	Known bool
}
