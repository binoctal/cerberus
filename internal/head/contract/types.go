package contract

// Contract is the AI-authored coverage standard for a session.
type Contract struct {
	Depth        string            // smoke | standard | thorough
	Scope        []string          // modules/paths to cover
	PathTypes    []string          // happy | alternative | boundary | edge
	ErrorScope   []string          // 4xx | validation | exception
	Boundaries   []string          // empty | zero | max | invalid | extreme
	Invariants   []InvariantRef    // pulled from project.yaml invariants
	Priorities   map[string]string // module → high|med|low
	CoverageGate Gate              // objective coverage threshold
}

// InvariantRef references a project invariant the contract must enforce.
type InvariantRef struct {
	ID          string
	Description string
}

// Gate is an objective coverage threshold (language-agnostic; each coverage
// provider measures, the contract only stores the threshold).
type Gate struct {
	Module          string
	LineThreshold   float64
	BranchThreshold float64
}

// Assessment is the Examiner's session-level verdict against a Contract.
type Assessment struct {
	Reached     bool    // contract satisfied?
	Gaps        []Gap   // what's missing
	CoveragePct float64 // objective coverage of gated module
	Reasoning   string
}

// Gap describes a coverage shortfall found during assessment.
type Gap struct {
	Kind   string // scope | pathtype | error | boundary | invariant | coverage
	Detail string
}
