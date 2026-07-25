package scout

// TargetInfo describes what the Scout should analyze.
type TargetInfo struct {
	URL      string // Base URL of the target service
	CodeRoot string // Optional path to project source code
	DBURL    string // Optional database connection URL
	Goal     string // What the user wants to test
}

// AnalyzeOutput is the assembly target produced from report_endpoint/
// report_page/declare_tech tool calls. It is no longer LLM-emitted JSON — the
// provider schema enforces the shape, so the legacy flexibleStrings drift
// absorption patch is gone.
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
