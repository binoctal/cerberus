package contract

// Contract is the AI-authored coverage standard for a session.
type Contract struct {
	Depth        string            `json:"depth"`         // smoke | standard | thorough
	Scope        []string          `json:"scope"`         // modules/paths to cover
	PathTypes    []string          `json:"path_types"`    // happy | alternative | boundary | edge
	ErrorScope   []string          `json:"error_scope"`   // 4xx | validation | exception
	Boundaries   []string          `json:"boundaries"`    // empty | zero | max | invalid | extreme
	Invariants   []InvariantRef    `json:"invariants"`    // pulled from project.yaml invariants
	Priorities   map[string]string `json:"priorities"`    // module → high|med|low
	CoverageGate Gate              `json:"coverage_gate"` // objective coverage threshold
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
	Reached     bool    `json:"reached"`     // contract satisfied?
	Gaps        []Gap   `json:"gaps"`        // what's missing
	CoveragePct float64 `json:"coverage_pct"` // objective coverage of gated module
	Reasoning   string  `json:"reasoning"`
}

// Gap describes a coverage shortfall found during assessment.
type Gap struct {
	Kind   string `json:"kind"`   // scope | pathtype | error | boundary | invariant | coverage
	Detail string `json:"detail"`
}
