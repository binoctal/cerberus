package scout

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// assemblePlan converts directPlan tool calls into a TestPlan plus the
// per-service set of roles already connected by a begin_case+ws_* group
// (covered), so WSCasesCovered can suppress redundant deterministic connects.
// Unknown/invalid calls are dropped, never panic.
func assemblePlan(calls []llm.ToolCall, goal, baseURL string, services []project.Service) (*agent.TestPlan, map[string]map[string]bool) {
	var cases []agent.TestCase
	covered := map[string]map[string]bool{}
	var open *agent.TestCase
	id := 0
	nextID := func() string { id++; return fmt.Sprintf("tc-%03d", id) }
	flush := func() {
		if open != nil {
			if open.Service != "" {
				for _, st := range open.Steps {
					if st.Action == "ws_connect" && st.Role != "" {
						if covered[open.Service] == nil {
							covered[open.Service] = map[string]bool{}
						}
						covered[open.Service][st.Role] = true
					}
				}
			}
			cases = append(cases, *open)
			open = nil
		}
	}

	for _, call := range calls {
		switch call.Name {
		case "test_http_endpoint":
			flush()
			cases = append(cases, assembleHTTP(call, nextID, services))
		case "check_invariant":
			flush()
			cases = append(cases, assembleInvariant(call, nextID))
		case "run_process":
			flush()
			cases = append(cases, assembleProcess(call, nextID))
		case "analyze_code":
			flush()
			cases = append(cases, assembleCode(call, nextID))
		case "check_file":
			flush()
			cases = append(cases, assembleFile(call, nextID))
		case "navigate":
			flush()
			cases = append(cases, assembleNavigate(call, nextID))
		case "begin_case":
			flush()
			open = &agent.TestCase{
				ID: nextID(), Name: strField(call, "name"),
				Expectation: strField(call, "expectation"), Action: "ws_flow",
				Service: strField(call, "service"),
			}
			// ws_* handled in Task 2; high-level-only tests never hit them.
		}
	}
	flush()
	cases = fillBody(cases, services) // retained: service.BodyTemplate fill
	return &agent.TestPlan{Goal: goal, Cases: cases, ProjectURL: baseURL}, covered
}

// --- field helpers (Input is map[string]any from provider JSON) ---

func strField(c llm.ToolCall, k string) string {
	if v, ok := c.Input[k].(string); ok {
		return v
	}
	return ""
}
func intField(c llm.ToolCall, k string) int {
	switch v := c.Input[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

//lint:ignore U1000 will be used in Task 2 (ws_* handlers)
func strSliceField(c llm.ToolCall, k string) []string {
	arr, ok := c.Input[k].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

//lint:ignore U1000 will be used in Task 2 (ws_* handlers)
func mapField(c llm.ToolCall, k string) map[string]any {
	if m, ok := c.Input[k].(map[string]any); ok {
		return m
	}
	return nil
}

// --- high-level assemblers ---

func assembleHTTP(c llm.ToolCall, nextID func() string, svcs []project.Service) agent.TestCase {
	method := strings.ToUpper(strField(c, "method"))
	path := strField(c, "path")
	tc := agent.TestCase{
		ID: nextID(), Name: fmt.Sprintf("%s %s", method, path), Target: path,
		Method: method, Body: strField(c, "body"),
		Expectation: formatHTTPExpectation(c), Service: strField(c, "service"),
	}
	if svc := attributeService(path, svcs); svc != "" {
		tc.Service = svc // deterministic override (replaces verifyServiceAttribution)
	}
	return tc
}

func formatHTTPExpectation(c llm.ToolCall) string {
	var parts []string
	if s := intField(c, "expect_status"); s != 0 {
		parts = append(parts, fmt.Sprintf("status %d", s))
	}
	if b := strField(c, "expect_body"); b != "" {
		parts = append(parts, fmt.Sprintf("body contains %q", b))
	}
	if len(parts) == 0 {
		return "Returns 2xx status code"
	}
	return strings.Join(parts, "; ")
}

func assembleInvariant(c llm.ToolCall, nextID func() string) agent.TestCase {
	desc := strField(c, "description")
	if id := strField(c, "invariant_id"); id != "" {
		desc = id
	}
	return agent.TestCase{
		ID: nextID(), Target: desc, Expectation: strField(c, "assertion"),
		Severity: strField(c, "severity"),
	}
}

func assembleProcess(c llm.ToolCall, nextID func() string) agent.TestCase {
	a := strField(c, "action") // build | exec (schema-enforced; test/lint via exec+cmd)
	return agent.TestCase{ID: nextID(), Action: "process_" + a, Target: strField(c, "cmd"), Expectation: strField(c, "expect")}
}

func assembleCode(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "code_" + strField(c, "action"), Target: strField(c, "target")}
}

func assembleFile(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "file_" + strField(c, "action"), Target: strField(c, "path"), Body: strField(c, "pattern"), Expectation: strField(c, "expect")}
}

func assembleNavigate(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "navigate", Target: strField(c, "path"), Expectation: strField(c, "expect")}
}
