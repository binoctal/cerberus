package scout

// TargetInfo describes what the Scout should analyze.
type TargetInfo struct {
	URL      string // Base URL of the target service
	CodeRoot string // Optional path to project source code
	DBURL    string // Optional database connection URL
	Goal     string // What the user wants to test
}

// AnalyzeOutput is the structured JSON the LLM returns for an Analyze call.
type AnalyzeOutput struct {
	Endpoints []EndpointInfo `json:"endpoints"`
	Pages     []PageInfo     `json:"pages"`
	TechStack []string       `json:"tech_stack"`
}

// EndpointInfo describes a discovered API endpoint.
type EndpointInfo struct {
	Method     string  `json:"method"`
	Path       string  `json:"path"`
	Confidence float64 `json:"confidence"`
}

// PageInfo describes a discovered page/route.
type PageInfo struct {
	Path       string  `json:"path"`
	Confidence float64 `json:"confidence"`
}

// PlanOutput is the structured JSON the LLM returns for a Plan call.
type PlanOutput struct {
	Cases []CaseInfo `json:"cases"`
}

// CaseInfo describes a generated test case.
type CaseInfo struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Target      string  `json:"target"`
	Method      string  `json:"method,omitempty"`
	Action      string  `json:"action,omitempty"`
	Expectation string  `json:"expectation"`
	Priority    float64 `json:"priority"`
}
