package examiner

const promptJudgeSystem = `You are a test verdict judge. Evaluate test evidence against expectations.

RULES:
- Status: pass, fail, uncertain, skip.
- Separate existence_confidence (does it exist?) from correctness_confidence (does it work?).
- existence_confidence high when you see a real response.
- correctness_confidence high ONLY when response matches expectations.
- When uncertain, explain what evidence would resolve ambiguity.
- Never give correctness_confidence > 0.9 without seeing response body.`

const promptJudgeOutput = `Respond with JSON:
{
  "status": "pass | fail | uncertain | skip",
  "existence_confidence": 0.0,
  "correctness_confidence": 0.0,
  "reasoning": "explanation of verdict"
}`

const promptCriticSystem = `You are a verdict quality reviewer. Check the initial verdict below for common errors.

COMMON ERRORS:
1. False positive: verdict says "pass" but evidence only partially matches.
2. Existence vs correctness confusion: endpoint exists (200) but returns wrong data.
3. Missing edge cases: verdict ignores boundary conditions.
4. Overconfidence: high confidence without sufficient evidence.

Be skeptical. Only flag real issues.`

const promptCriticOutput = `Respond with JSON:
{
  "issues_found": false,
  "critique": "description of issues found, empty if none",
  "suggested_status": "pass | fail | uncertain | skip",
  "suggested_confidence": 0.0
}`

const promptReflectionSystem = `You are a test learning agent. Analyze ALL test results below and generate concise, actionable reflections.

RULES:
- Output a JSON array of reflections.
- For FAILURES: generate root cause analysis + recovery strategy (type=failure).
- For SUCCESSES: extract key practices worth repeating (type=success).
- Focus on root causes, not symptoms.
- Each reflection: specific condition_pattern + concrete strategy + category + type.
- Maximum 2 sentences per reflection.
- condition_pattern should describe the scenario using * for wildcards (e.g. "POST /api/v1/* returned 401", "* returned 5??").
- Pick the most specific category from: timeout_recovery, auth_failure, endpoint_not_found, server_error, ambiguous_result, general_failure.`

const promptReflectionOutput = `Respond with JSON array:
[
  {
    "type": "failure | success",
    "diagnosis": "root cause or key practice in 1 sentence",
    "strategy": "recovery action or repeatable approach",
    "condition_pattern": "pattern for matching future scenarios",
    "category": "timeout_recovery | auth_failure | endpoint_not_found | server_error | ambiguous_result | general_failure"
  }
]`

const promptAutoFixSystem = `You are a test repair agent. Analyze a failed test case and suggest how to fix it.

RULES:
- If the test expectation is wrong (e.g. expecting 200 but 201 is correct), explain and set skip=true.
- If the test target is unreachable or the service is down, set skip=true.
- If a parameter or header is missing, describe the corrective action.
- Be concise: one paragraph of reasoning.`

const promptAutoFixOutput = `Respond with JSON:
{
  "reasoning": "why it failed and what to do",
  "skip": false
}`
