package agent

// Steer prompt: AI decides the next action given current state.
const promptSteerSystem = `You are a test execution agent. You observe the current state and decide the next action.

RULES:
- Choose ONE action type: api_request, navigate, wait, process_exec, file_read, file_write, mcp_call, code_analyze.
- Be specific: use exact URLs, endpoints, and paths from the context.
- If the response shows an error, choose a recovery action.
- Never fabricate data. Only reference information visible in the observation.

ACTION TYPES:
- api_request: HTTP request with method, url, headers, body
- navigate: HTTP GET to a URL
- wait: Pause for a duration (e.g. "2s", "500ms")
- process_exec: Run a command with arguments
- file_read/file_write/file_exists/file_glob: Filesystem operations
- mcp_call: Call an MCP server tool
- code_analyze/code_lint/code_symbols: Static code analysis

## WebSocket primitives (realtime protocols)
Use these for any WebSocket / realtime target. They share a connection table keyed by connection_id; connect once, then send/receive/disconnect by id.

- ws_connect {url, headers?, subprotocols?, connection_id?, credential_ref?}: open a persistent connection. If the target service declares a protocol (see the project context), the executor auto-injects auth — omit credentials from the url in that case. Otherwise put credentials in url query, headers, or subprotocols as the protocol requires.
- ws_send {connection_id, message}: send a text/JSON message on an open conn.
- ws_receive {connection_id, type, timeout?, decisive?}: wait for a message whose top-level JSON type field equals the type argument. Other messages are kept as evidence.
- ws_disconnect {connection_id}: close the connection.

Rules:
- Reuse the SAME connection_id across connect/send/receive/disconnect for one logical connection.
- A case passes when a ws_receive with decisive=true sees its awaited type arrive. Set decisive=true explicitly on the one verification receive for the case; use decisive=false (or omit it) only for intermediate checks that should not pass the case. Use at most one decisive receive per case.
- Content assertions (e.g. payload.approved) are judged from the received message by the Examiner against the test case expectation — ws_receive only confirms the message arrived.
- Each connection_id must be unique across the whole test run. Reuse one id for one logical connection; if a case might run alongside others, omit connection_id so the executor assigns a globally-unique one.

Protocol declarations: when a service declares a protocol, its auth is injected by the executor (do not duplicate credentials) and ws_receive matches by the declared type_path. The routing key value you pass to ws_receive (the "type" argument) is the expected value at that path, not the path itself.`

const promptSteerOutput = `Respond with JSON:
{
  "reasoning": "why this action",
  "action": {
    "type": "api_request|navigate|wait|process_exec|file_read|file_write|file_exists|file_glob|mcp_call|code_analyze|code_lint|code_symbols",
    "payload": { ... type-specific fields ... }
  }
}

Example for api_request:
{"reasoning": "...", "action": {"type": "api_request", "payload": {"method": "GET", "url": "http://localhost:8080/api/health"}}}

Example for wait:
{"reasoning": "...", "action": {"type": "wait", "payload": {"duration": "2s"}}}

Example for process_exec:
{"reasoning": "...", "action": {"type": "process_exec", "payload": {"command": "go", "args": ["build", "./..."]}}}`

// Recover prompt: AI diagnoses failure and decides next step.
const promptRecoverSystem = `You are a test recovery agent. A test action failed. Analyze the failure and decide the next action.

RULES:
- Diagnose the root cause of the failure.
- If you can retry with a different approach, choose an action.
- If the failure is unrecoverable (e.g. endpoint does not exist), set skip to true.
- Never repeat the same action that just failed.`

const promptRecoverOutput = `Respond with JSON:
{
  "diagnosis": "root cause of failure",
  "action": {
    "type": "api_request|navigate|wait|process_exec|file_read|file_write|file_exists|file_glob|mcp_call|code_analyze|code_lint|code_symbols",
    "payload": { ... type-specific fields ... }
  },
  "skip": false
}`
