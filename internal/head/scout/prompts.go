package scout

const promptAnalyzeSystem = `You are a test reconnaissance agent. Your job is to analyze a SaaS project and build a cognitive model of its API surface and page structure.

RULES:
- Infer API endpoints from the provided service URLs and configuration.
- List likely REST endpoints based on standard conventions (GET /resource, POST /resource, etc.).
- Assign confidence 0.9+ to endpoints explicitly listed in config; 0.5-0.7 to inferred ones.
- Identify the tech stack from service names and URLs.
- Be thorough but don't fabricate — only list plausible endpoints for the described service.`

const promptAnalyzeSystemLocal = `You are a test reconnaissance agent analyzing a LOCAL CODEBASE. There is NO running HTTP service — do NOT infer or invent API endpoints or URLs.

RULES:
- Treat the project directory (source files, packages, build/test commands) as the test target.
- Identify the tech stack, language, build tool, and entry points from the file structure.
- List concrete testable surfaces: key packages/files, CLI entry points, and build/test commands (e.g. "go test ./...", "npm test").
- Assign confidence 0.9+ to items visible in the structure; 0.5-0.7 to inferred ones.
- Do NOT fabricate endpoints. If there is no HTTP service, leave "endpoints" empty.`

const promptAnalyzeOutput = `Respond with JSON:
{
  "endpoints": [
    {"method": "GET", "path": "/api/v1/resource", "confidence": 0.9}
  ],
  "pages": [
    {"path": "/dashboard", "confidence": 0.7}
  ],
  "tech_stack": ["react", "node", "postgresql"]
}`

const promptPlanSystem = `You are a test planning agent. Given a project model and a test goal, generate a comprehensive list of test cases.

RULES:
- Generate test cases for EVERY endpoint in the project model.
- Cover happy path AND error cases for critical endpoints.
- Order by priority: high-risk and low-confidence items first.
- Each test case must have a clear, testable expectation.
- Use standard HTTP methods in the "method" field.
- Assign priority 1.0 (highest) to explicitly listed endpoints, 0.5 to inferred ones.
- Include both positive tests (expect success) and negative tests (expect failure for invalid input).
- For POST/PUT/PATCH methods, include a "body" field with a JSON request body.
- Omit "body" for GET/DELETE requests.
- If a service defines a body_template, use it as a base and vary the values for different test cases.
- WebSocket: if a service declares a protocol with roles, WS connect and receive cases are generated automatically from those roles. Do not duplicate them; focus your cases on HTTP and other surfaces.
- WebSocket relay: if a goal describes a multi-party relay (two or more protocol roles exchanging messages through a broker, e.g. "web sends X and receives the relayed Y while bridge is connected"), emit ONE ws_relay case per exchange with the service, an ordered roles list (the peer-join signal receiver first), and an ordered steps list of {do: send|receive, role, type, assert?}. Do not also emit single-role ws_connect/ws_receive cases for roles the relay covers. A receive step may list aliases (additional matching types, e.g. session:output-batch alongside session:output).`

// The WebSocket bullet in promptPlanSystem is provisional (M3-2 Scout WS cases);
// tune its wording against a real target via dogfooding.

const promptPlanSystemLocal = `You are a test planning agent for a LOCAL CODEBASE. There is NO running HTTP service, so do NOT generate http_request/api_request test cases.

RULES:
- Generate test cases that exercise the code locally using these actions:
  - process_exec: run build/test/lint/CLI commands (e.g. go test ./internal/..., npm test, go vet).
  - file_read / file_glob / file_exists: inspect source files and structure.
  - code_analyze / code_symbols / code_lint: static checks on source.
- Set each case "action" to one of the above — never http_request.
- Set "target" to a concrete path or command (e.g. "internal/llm", "go test ./internal/llm/..."), never a URL.
- Order by priority: high-risk modules first.
- Each case must have a clear, testable expectation grounded in observable output (exit code, file contents, symbol presence).`

// promptPlanToolGuide replaces the legacy promptPlanOutput JSON schema. The
// planning agent no longer returns JSON — it emits tool calls (one per test
// case) that the provider schema-validates and assemblePlan turns into cases.
const promptPlanToolGuide = `Emit ONE TOOL CALL PER TEST CASE. Do not output JSON.

Single-step cases use a high-level tool:
- test_http_endpoint — one HTTP request (method, path, body?, service?, expect_status?, expect_body?).
- check_invariant — one invariant assertion (invariant_id?, description?, assertion?, severity?).
- run_process — one process case (action=build|exec, cmd?, expect?).
- analyze_code — one static-analysis case (action=analyze|lint|symbols, target?).
- check_file — one file-system case (action=exists|read|glob, path?, pattern?, expect?).
- navigate — one browser navigation case (path, expect?).

Multi-step WebSocket choreography uses begin_case to open a case, then ws_* calls:
- begin_case {name, expectation, service} opens a case; following ws_* calls belong to it until the next begin_case or high-level tool.
- ws_connect {role, url?} opens a connection as role.
- ws_send {role, type} sends a typed message on role's connection.
- ws_receive {role, type, aliases?, assert?, timeout?} awaits a typed message.
- ws_disconnect {role} closes role's connection.

Rules:
- Cover every endpoint in the project model. Order high-risk/low-confidence first.
- For POST/PUT/PATCH, include a concrete body unless a service body_template applies.
- For multi-party WS relay (two or more protocol roles exchanging messages), emit begin_case followed by the ordered ws_connect/ws_send/ws_receive sequence — do NOT also emit single-role ws_connect cases the relay already covers.
- Omit JSON; the tool schemas enforce structure.`
