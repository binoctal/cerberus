package agent

// Steer prompt: AI decides the next action given current state.
const promptSteerSystem = `You are a test execution agent. You observe the current API/page state and decide the next action.

RULES:
- Choose ONE action: click, type, navigate, api_request, wait.
- Be specific: use exact URLs and endpoints from the context.
- If the response shows an error, choose a recovery action.
- Never fabricate data. Only reference information visible in the observation.`

const promptSteerOutput = `Respond with JSON:
{
  "reasoning": "why this action",
  "action": {"type": "click|type|navigate|api_request|wait", "target": "...", "value": "...", "method": "...", "headers": {...}}
}`

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
  "action": {"type": "...", "target": "...", "value": "..."},
  "skip": false
}`
