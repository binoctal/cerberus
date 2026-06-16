# Architecture

Cerberus uses a three-head architecture inspired by its namesake: Scout
discovers, Agent executes, Examiner judges. A session orchestrates all three
in sequence.

## Three-Head Model

### Scout -- Analyze and Plan

Builds a `ProjectModel` from config and AI inference, then generates a
`TestPlan`.

**Analyze phase:**

1. Loads ground truth from `.cerberus/project.yaml`
2. If info score < 0.7, uses AI to discover additional endpoints
3. Merges and deduplicates results

**Plan phase:**

- **Direct mode** (default): single AI call with deterministic fallback
- **Deep planning** (`--deep-plan`): Tree-of-Thought beam search
  - BeamWidth: 3, GenerateN: 5, MaxSteps: 3
  - Each step: Propose -> Evaluate (70% AI + 30% coverage) -> Select top-k

### Agent -- Execute and Recover

Runs the test plan via a ReAct (Reason-Act-Observe) loop.

**Per test case:**

1. **Rule engine** (zero tokens): 15 built-in rules map test cases to actions
   deterministically
2. **ReAct loop** (up to 3 steer attempts): AI selects action, executor runs it,
   observe result
3. **Recovery**: on failure, LLM suggests recovery with L3 procedural memory

**Escalation checkpoints:**

| Checkpoint           | Trigger                          |
|----------------------|----------------------------------|
| Budget warning       | Token usage > 80%                |
| Systemic failure     | 5 consecutive failures           |
| Destructive risk     | DELETE/DROP/rm/file-write        |
| Target unreachable   | 3 consecutive connection errors  |

### Examiner -- Judge and Learn

**Judge** uses Self-Refine evaluation:

1. Initial judgment by main LLM
2. Early stop if confidence >= 0.9
3. Optional critique by a separate critic model

**VerdictPolicy** degrades gracefully through 3 levels:
1. Self-Refine (normal)
2. Checker-only (HTTP 2xx + uncertain -> pass at 0.5 confidence)
3. Pending review

The `Settings.ConfidenceThreshold` config knob (v0.3.0) downgrades pass
verdicts whose `CorrectnessConfidence` falls below the threshold to uncertain.

**Learner** generates L3 procedural memories (reflexion) from results, quality-gates
them, and stores them in SQLite. Future sessions match and inject relevant strategies.

## MultiExecutor Pipeline

Every action passes through 4 layers:

```
Policy -> Sandbox -> Route -> Anomaly Detection
```

1. **Policy** validates action against `.cerberus/policy.yaml`
2. **Sandbox** isolates execution (Linux namespaces, fallback to no-op)
3. **Route** dispatches to the registered executor plugin
4. **Anomaly** flags unusual results via the escalation gate

### Built-in Executor Plugins

10 plugins: `http`, `process`, `file`, `mcp`, `code`, `wait`, `browser`,
`database`, `graphql`, `websocket`. Browser is optional (requires Playwright).

### Plugin System

`ExecutorPlugin` interface: `Name()`, `Executor()`, `ActionTypes()`.
`RulePlugin` interface for custom deterministic rule matching.
`ExtendedRuleEngine` tries rule plugins before built-in rules.

## Memory System

| Type         | Scope           | Storage             | Description                                                          |
|--------------|-----------------|---------------------|----------------------------------------------------------------------|
| Episodic     | Session         | SQLite              | Test results and observations                                        |
| Semantic     | Cross-session   | SQLite + embedding  | Char-trigram embeddings, cosine similarity search (L2)               |
| Procedural   | Cross-session   | SQLite              | Learned strategies (L3)                                              |

**L2 Semantic** memory stores reflections as char-trigram embeddings; Scout's
`buildEpisodicContext` appends project-scoped cosine search results to the plan
context (added v0.4.0, project-scoped search in v0.6.0).

The `StrategyMatcher` injects relevant L3 memories into recovery prompts using
glob/substring pattern matching.

## Session Lifecycle

```
Scout.Analyze -> Scout.Plan -> Agent.Execute (parallel) -> Examiner.Judge -> Examiner.Learn
```

- Parallel execution respects `DependsOn` ordering with cascade skip
- Worker pool bounded by `--workers` (default 4)
- Per-case timeout: 2 minutes (default)
- Token budget tracked across all LLM calls

## Escalation Gate

`Gate` interface with two implementations:

- **NoOpGate** (CLI mode): always continues
- **MCPGate** (MCP mode): blocks goroutine until human decision arrives via `cerberus_decide`
