package scout

import (
	"encoding/json"
	"fmt"
)

// flexibleStrings decodes a field the prompt documents as a string array but
// that non-Claude LLMs frequently emit as an array of objects (e.g. tech_stack:
// [{"language":"go","confidence":1.0}, {"build_tool":"make",...}]).
//
// It accepts both shapes, extracting a string value from each object element
// (the identifier — confidence/inferred are numeric/bool and ignored). This
// keeps Analyze from degrading to config-only when the model embellishes.
type flexibleStrings []string

func (f *flexibleStrings) UnmarshalJSON(data []byte) error {
	var flat []string
	if err := json.Unmarshal(data, &flat); err == nil {
		*f = flat
		return nil
	}
	var objs []map[string]any
	if err := json.Unmarshal(data, &objs); err != nil {
		return fmt.Errorf("expected string array or object array: %w", err)
	}
	for _, obj := range objs {
		if s := firstStringValue(obj); s != "" {
			*f = append(*f, s)
		}
	}
	return nil
}

// firstStringValue returns the first string-typed value in the map. LLM tech
// entries pair an identifier with numeric confidence / boolean inferred flags,
// so the only string present is the identifier itself.
func firstStringValue(m map[string]any) string {
	for _, v := range m {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// TargetInfo describes what the Scout should analyze.
type TargetInfo struct {
	URL      string // Base URL of the target service
	CodeRoot string // Optional path to project source code
	DBURL    string // Optional database connection URL
	Goal     string // What the user wants to test
}

// AnalyzeOutput is the structured JSON the LLM returns for an Analyze call.
type AnalyzeOutput struct {
	Endpoints []EndpointInfo  `json:"endpoints"`
	Pages     []PageInfo      `json:"pages"`
	TechStack flexibleStrings `json:"tech_stack"`
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
	Body        string  `json:"body,omitempty"`
}
