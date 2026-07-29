package scout

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// assemblePlan converts directPlan tool calls into a TestPlan plus the
// per-service set of roles already connected by a begin_case+ws_* group
// (covered), so WSCasesCovered can suppress redundant deterministic connects.
// Unknown/invalid calls are dropped, never panic.
func assemblePlan(calls []llm.ToolCall, goal, baseURL string, services []project.Service) (*agent.TestPlan, map[string]map[string]bool, map[string]map[string]string, map[string]map[string]string) {
	var cases []agent.TestCase
	covered := map[string]map[string]bool{}
	// A1 Phase 2: side table mirroring covered, carrying the ID of the sound
	// LLM case that covered each (svc, role), so WSCasesCovered can emit a lazy
	// fallback bound to it. covered stays bool; this adds only the binding.
	coveringCase := map[string]map[string]string{}
	// A1 #4: HTTP coverage side table — service -> path -> ID of the LLM HTTP
	// case that covered the endpoint, so HTTPCasesCovered can bind a lazy smoke
	// fallback to it. Deduped by (service, path): one smoke per endpoint.
	httpCovering := map[string]map[string]string{}
	// service -> declared protocol, so flush can judge ws_flow soundness without
	// re-scanning services per case.
	svcProtos := map[string]*project.Protocol{}
	for _, s := range services {
		if s.Protocol != nil {
			svcProtos[s.Name] = s.Protocol
		}
	}
	var open *agent.TestCase
	id := 0
	nextID := func() string { id++; return fmt.Sprintf("tc-%03d", id) }
	flush := func() {
		if open != nil {
			if open.Action == "ws_flow" && len(open.Steps) == 0 {
				// A begin_case the LLM opened with no following ws_* calls.
				// Drop it: a 0-step ws_flow case is not a real case — it would
				// waste an Agent cycle and confuse the Examiner. (Defense side
				// of ws_flow emission stability; the prompt handles guidance.)
				open = nil
				return
			}
			if open.Service != "" {
				// A1 unsound-fallback (Phase 1): only a SOUND ws_flow suppresses
				// the deterministic fallback for the roles it connects. An unsound
				// case (a ws_receive of an invented type) stays in the plan but
				// does not mark its roles covered, so WSCasesCovered still emits
				// the deterministic fallback for them.
				if llmWSFlowSound(open, svcProtos[open.Service], goal) {
					for _, st := range open.Steps {
						if st.Action == "ws_connect" && st.Role != "" {
							if covered[open.Service] == nil {
								covered[open.Service] = map[string]bool{}
							}
							covered[open.Service][st.Role] = true
							// A1 Phase 2: record which sound case covered this
							// role, so WSCasesCovered can bind a lazy fallback.
							if coveringCase[open.Service] == nil {
								coveringCase[open.Service] = map[string]string{}
							}
							coveringCase[open.Service][st.Role] = open.ID
						}
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
			hc := assembleHTTP(call, nextID, services)
			if hc.Service != "" && strings.HasPrefix(hc.Target, "/") {
				if httpCovering[hc.Service] == nil {
					httpCovering[hc.Service] = map[string]string{}
				}
				if _, dup := httpCovering[hc.Service][hc.Target]; !dup {
					httpCovering[hc.Service][hc.Target] = hc.ID
				}
			}
			cases = append(cases, hc)
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
			svcName := llm.StrField(call, "service")
			open = &agent.TestCase{
				ID: nextID(), Name: llm.StrField(call, "name"),
				Expectation: llm.StrField(call, "expectation"), Action: "ws_flow",
				Service: svcName,
				// ws_connect dials tc.Target (stepToAction); the LLM emits the
				// service NAME, so resolve it to the service URL here.
				Target: serviceURLByName(svcName, services),
			}
			// ws_* handled in Task 2; high-level-only tests never hit them.
		case "ws_connect":
			if open == nil {
				continue
			}
			open.Steps = append(open.Steps, agent.TestStep{
				Action: "ws_connect", ConnectionID: llm.StrField(call, "role"), Role: llm.StrField(call, "role"),
				URL: llm.StrField(call, "url"),
			})
		case "ws_send":
			if open == nil {
				continue
			}
			open.Steps = append(open.Steps, agent.TestStep{
				Action: "ws_send", ConnectionID: llm.StrField(call, "role"), Message: wsSendBody(llm.StrField(call, "type")),
			})
		case "ws_receive":
			if open == nil {
				continue
			}
			open.Steps = append(open.Steps, agent.TestStep{
				Action: "ws_receive", ConnectionID: llm.StrField(call, "role"),
				Type: llm.StrField(call, "type"), Aliases: llm.StrSliceField(call, "aliases"),
				Asserts: llm.MapField(call, "assert"), Timeout: llm.IntField(call, "timeout"),
			})
		case "ws_disconnect":
			if open == nil {
				continue
			}
			open.Steps = append(open.Steps, agent.TestStep{
				Action: "ws_disconnect", ConnectionID: llm.StrField(call, "role"),
			})
		}
	}
	flush()
	cases = fillBody(cases, services) // retained: service.BodyTemplate fill
	return &agent.TestPlan{Goal: goal, Cases: cases, ProjectURL: baseURL}, covered, coveringCase, httpCovering
}

// --- field helpers live in internal/llm/toolfield.go (shared with Agent) ---

// assembleAnalyze converts Analyze tool calls (report_endpoint/report_page/
// declare_tech) into an AnalyzeOutput. The provider schema enforces string
// arrays for declare_tech, so no drift absorption is needed. Unknown calls are
// dropped, never panic.
func assembleAnalyze(calls []llm.ToolCall) AnalyzeOutput {
	var out AnalyzeOutput
	for _, c := range calls {
		switch c.Name {
		case "report_endpoint":
			out.Endpoints = append(out.Endpoints, EndpointInfo{
				Method:     llm.StrField(c, "method"),
				Path:       llm.StrField(c, "path"),
				Confidence: llm.NumField(c, "confidence"),
			})
		case "report_page":
			out.Pages = append(out.Pages, PageInfo{
				Path:       llm.StrField(c, "path"),
				Confidence: llm.NumField(c, "confidence"),
			})
		case "declare_tech":
			out.TechStack = llm.StrSliceField(c, "stack")
		}
	}
	return out
}

// assembleContract converts coverage-contract tool calls (declare_scope/
// path_types/error_scope/boundaries, set_priority, set_coverage_gate) into a
// contract.Contract. Priorities is initialized non-nil so set_priority can
// populate it; the set_priority schema forces map[string][]string, so no
// Priorities.UnmarshalJSON dual-shape absorption is needed. Unknown calls are
// dropped, never panic.
func assembleContract(calls []llm.ToolCall, depth string, invs []contract.InvariantRef) *contract.Contract {
	c := &contract.Contract{Depth: depth, Priorities: contract.Priorities{}, Invariants: invs}
	for _, call := range calls {
		switch call.Name {
		case "declare_scope":
			c.Scope = llm.StrSliceField(call, "modules")
		case "declare_path_types":
			c.PathTypes = llm.StrSliceField(call, "types")
		case "declare_error_scope":
			c.ErrorScope = llm.StrSliceField(call, "scopes")
		case "declare_boundaries":
			c.Boundaries = llm.StrSliceField(call, "boundaries")
		case "set_priority":
			c.Priorities[llm.StrField(call, "bucket")] = llm.StrSliceField(call, "modules")
		case "set_coverage_gate":
			c.CoverageGate = contract.Gate{
				Module:          llm.StrField(call, "module"),
				LineThreshold:   llm.NumField(call, "line_threshold"),
				BranchThreshold: llm.NumField(call, "branch_threshold"),
			}
		}
	}
	return c
}

// --- high-level assemblers ---

// serviceURLByName returns the URL of the named service, or "" if not found.
// Used by begin_case so a ws_flow case's Target is the dial URL stepToAction
// uses for ws_connect (the LLM emits the service NAME, not the URL).
func serviceURLByName(name string, services []project.Service) string {
	for _, s := range services {
		if s.Name == name {
			return s.URL
		}
	}
	return ""
}

func assembleHTTP(c llm.ToolCall, nextID func() string, svcs []project.Service) agent.TestCase {
	method := strings.ToUpper(llm.StrField(c, "method"))
	path := llm.StrField(c, "path")
	tc := agent.TestCase{
		ID: nextID(), Name: fmt.Sprintf("%s %s", method, path), Target: path,
		Method: method, Body: llm.StrField(c, "body"),
		Expectation: formatHTTPExpectation(c), Service: llm.StrField(c, "service"),
	}
	if svc := attributeService(path, svcs); svc != "" {
		tc.Service = svc // deterministic override (replaces verifyServiceAttribution)
	}
	return tc
}

func formatHTTPExpectation(c llm.ToolCall) string {
	var parts []string
	if s := llm.IntField(c, "expect_status"); s != 0 {
		parts = append(parts, fmt.Sprintf("status %d", s))
	}
	if b := llm.StrField(c, "expect_body"); b != "" {
		parts = append(parts, fmt.Sprintf("body contains %q", b))
	}
	if len(parts) == 0 {
		return "Returns 2xx status code"
	}
	return strings.Join(parts, "; ")
}

func assembleInvariant(c llm.ToolCall, nextID func() string) agent.TestCase {
	desc := llm.StrField(c, "description")
	if id := llm.StrField(c, "invariant_id"); id != "" {
		desc = id
	}
	return agent.TestCase{
		ID: nextID(), Target: desc, Expectation: llm.StrField(c, "assertion"),
		Severity: llm.StrField(c, "severity"),
	}
}

func assembleProcess(c llm.ToolCall, nextID func() string) agent.TestCase {
	a := llm.StrField(c, "action") // build | exec (schema-enforced; test/lint via exec+cmd)
	return agent.TestCase{ID: nextID(), Action: "process_" + a, Target: llm.StrField(c, "cmd"), Expectation: llm.StrField(c, "expect")}
}

func assembleCode(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "code_" + llm.StrField(c, "action"), Target: llm.StrField(c, "target")}
}

func assembleFile(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "file_" + llm.StrField(c, "action"), Target: llm.StrField(c, "path"), Body: llm.StrField(c, "pattern"), Expectation: llm.StrField(c, "expect")}
}

func assembleNavigate(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "navigate", Target: llm.StrField(c, "path"), Expectation: llm.StrField(c, "expect")}
}
