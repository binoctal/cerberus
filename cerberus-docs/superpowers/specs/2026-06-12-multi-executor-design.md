# Multi-Executor Architecture Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend Cerberus from HTTP-only testing to a universal multi-executor testing framework capable of testing any project type — including itself.

**Architecture:** Plugin-registry MultiExecutor dispatches TypedActions to specialized executors (HTTP, Process, File, MCP, Code). Three-layer defense (Policy → Sandbox → Escalation) secures all execution. Scout gains type-aware planning to generate non-HTTP test cases.

**Tech Stack:** Go 1.25, criyle/go-sandbox (Linux namespace+seccomp+cgroup), elastic/go-seccomp-bpf (fallback), go/parser+go/ast (code analysis)

---

## Section 1: Core Type System

### 1.1 ActionType

```go
// internal/head/agent/types.go

type ActionType string

const (
    // HTTP / API
    ActionAPIRequest ActionType = "api_request"
    ActionNavigate   ActionType = "navigate"
    ActionWait       ActionType = "wait"

    // Process execution
    ActionProcessExec  ActionType = "process_exec"
    ActionProcessBuild ActionType = "process_build"

    // File operations
    ActionFileRead   ActionType = "file_read"
    ActionFileWrite  ActionType = "file_write"
    ActionFileExists ActionType = "file_exists"
    ActionFileGlob   ActionType = "file_glob"

    // MCP calls
    ActionMCPCall ActionType = "mcp_call"

    // Code analysis
    ActionCodeAnalyze ActionType = "code_analyze"
    ActionCodeLint    ActionType = "code_lint"
    ActionCodeSymbols ActionType = "code_symbols"
)
```

Note: Browser-specific actions (click, input) are not part of the initial executor set. They can be added in future phases by registering a BrowserExecutor.

### 1.2 Sum Type Action System

```go
// internal/head/agent/actions.go — new file

// ActionEnvelope is the unified envelope for serialization and routing.
// Type determines how to unmarshal Raw into a concrete TypedAction.
type ActionEnvelope struct {
    Type ActionType      `json:"type"`
    Raw  json.RawMessage `json:"raw"`
}

// TypedAction is the interface for all concrete action types.
type TypedAction interface {
    ActionType() ActionType
    Validate() error
    Target() string
}

// --- HTTP Actions ---

type HTTPAction struct {
    Method  string            `json:"method,omitempty"`
    URL     string            `json:"url"`
    Headers map[string]string `json:"headers,omitempty"`
    Body    string            `json:"body,omitempty"`
}

func (a HTTPAction) ActionType() ActionType { return ActionAPIRequest }
func (a HTTPAction) Target() string         { return a.URL }
func (a HTTPAction) Validate() error {
    if a.URL == "" { return fmt.Errorf("url is required") }
    return nil
}

type NavigateAction struct {
    URL string `json:"url"`
}

func (a NavigateAction) ActionType() ActionType { return ActionNavigate }
func (a NavigateAction) Target() string         { return a.URL }
func (a NavigateAction) Validate() error {
    if a.URL == "" { return fmt.Errorf("url is required") }
    return nil
}

type WaitAction struct {
    Duration string `json:"duration"`
}

func (a WaitAction) ActionType() ActionType { return ActionWait }
func (a WaitAction) Target() string         { return "" }
func (a WaitAction) Validate() error        { return nil }

// --- Process Actions ---

type ProcessExecAction struct {
    Command string            `json:"command"`
    Args    []string          `json:"args,omitempty"`
    Env     map[string]string `json:"env,omitempty"`
    WorkDir string            `json:"work_dir,omitempty"`
    Timeout string            `json:"timeout,omitempty"` // e.g. "30s", "5m"
}

func (a ProcessExecAction) ActionType() ActionType { return ActionProcessExec }
func (a ProcessExecAction) Target() string         { return a.Command }
func (a ProcessExecAction) Validate() error {
    if a.Command == "" { return fmt.Errorf("command is required") }
    if a.Timeout != "" {
        if _, err := time.ParseDuration(a.Timeout); err != nil {
            return fmt.Errorf("invalid timeout %q: %w", a.Timeout, err)
        }
    }
    return nil
}

// --- File Actions ---

type FileReadAction struct {
    Path string `json:"path"`
}

func (a FileReadAction) ActionType() ActionType { return ActionFileRead }
func (a FileReadAction) Target() string         { return a.Path }
func (a FileReadAction) Validate() error {
    if a.Path == "" { return fmt.Errorf("path is required") }
    return nil
}

type FileWriteAction struct {
    Path    string `json:"path"`
    Content string `json:"content"`
}

func (a FileWriteAction) ActionType() ActionType { return ActionFileWrite }
func (a FileWriteAction) Target() string         { return a.Path }
func (a FileWriteAction) Validate() error {
    if a.Path == "" { return fmt.Errorf("path is required") }
    return nil
}

type FileExistsAction struct {
    Path string `json:"path"`
}

func (a FileExistsAction) ActionType() ActionType { return ActionFileExists }
func (a FileExistsAction) Target() string         { return a.Path }
func (a FileExistsAction) Validate() error {
    if a.Path == "" { return fmt.Errorf("path is required") }
    return nil
}

type FileGlobAction struct {
    Pattern string `json:"pattern"`
}

func (a FileGlobAction) ActionType() ActionType { return ActionFileGlob }
func (a FileGlobAction) Target() string         { return a.Pattern }
func (a FileGlobAction) Validate() error {
    if a.Pattern == "" { return fmt.Errorf("pattern is required") }
    return nil
}

// --- MCP Actions ---

type MCPCallAction struct {
    Server string         `json:"server"`
    Method string         `json:"method"`
    Params map[string]any `json:"params,omitempty"`
}

func (a MCPCallAction) ActionType() ActionType { return ActionMCPCall }
func (a MCPCallAction) Target() string         { return a.Server + "/" + a.Method }
func (a MCPCallAction) Validate() error {
    if a.Method == "" { return fmt.Errorf("method is required") }
    return nil
}

// --- Code Actions ---

type CodeAnalyzeAction struct {
    TargetPath string   `json:"target_path"`
    Language   string   `json:"language,omitempty"`
    Checks     []string `json:"checks,omitempty"`
}

func (a CodeAnalyzeAction) ActionType() ActionType { return ActionCodeAnalyze }
func (a CodeAnalyzeAction) Target() string         { return a.TargetPath }
func (a CodeAnalyzeAction) Validate() error {
    if a.TargetPath == "" { return fmt.Errorf("target_path is required") }
    return nil
}

type CodeLintAction struct {
    TargetPath string   `json:"target_path"`
    Language   string   `json:"language,omitempty"`
    Rules      []string `json:"rules,omitempty"`
}

func (a CodeLintAction) ActionType() ActionType { return ActionCodeLint }
func (a CodeLintAction) Target() string         { return a.TargetPath }
func (a CodeLintAction) Validate() error {
    if a.TargetPath == "" { return fmt.Errorf("target_path is required") }
    return nil
}

type CodeSymbolsAction struct {
    TargetPath string `json:"target_path"`
    Language   string `json:"language,omitempty"`
}

func (a CodeSymbolsAction) ActionType() ActionType { return ActionCodeSymbols }
func (a CodeSymbolsAction) Target() string         { return a.TargetPath }
func (a CodeSymbolsAction) Validate() error {
    if a.TargetPath == "" { return fmt.Errorf("target_path is required") }
    return nil
}
```

### 1.3 Serialization Registry

```go
// internal/head/agent/actions.go — continued

// unmarshalRegistry maps ActionType to a factory function that returns
// a pointer to a zero-valued concrete TypedAction for deserialization.
// Avoids reflect; each factory is a typed constructor.
var unmarshalRegistry = map[ActionType]func() TypedAction{
    ActionAPIRequest:   func() TypedAction { return &HTTPAction{} },
    ActionNavigate:     func() TypedAction { return &NavigateAction{} },
    ActionWait:         func() TypedAction { return &WaitAction{} },
    ActionProcessExec:  func() TypedAction { return &ProcessExecAction{} },
    ActionFileRead:     func() TypedAction { return &FileReadAction{} },
    ActionFileWrite:    func() TypedAction { return &FileWriteAction{} },
    ActionFileExists:   func() TypedAction { return &FileExistsAction{} },
    ActionFileGlob:     func() TypedAction { return &FileGlobAction{} },
    ActionMCPCall:      func() TypedAction { return &MCPCallAction{} },
    ActionCodeAnalyze:  func() TypedAction { return &CodeAnalyzeAction{} },
    ActionCodeLint:     func() TypedAction { return &CodeLintAction{} },
    ActionCodeSymbols:  func() TypedAction { return &CodeSymbolsAction{} },
}

func UnmarshalAction(envelope ActionEnvelope) (TypedAction, error) {
    factory, ok := unmarshalRegistry[envelope.Type]
    if !ok {
        return nil, fmt.Errorf("unknown action type: %s", envelope.Type)
    }
    action := factory()
    if err := json.Unmarshal(envelope.Raw, action); err != nil {
        return nil, fmt.Errorf("unmarshal %s: %w", envelope.Type, err)
    }
    if err := action.Validate(); err != nil {
        return nil, err
    }
    return action, nil
}

func MarshalAction(action TypedAction) (ActionEnvelope, error) {
    raw, err := json.Marshal(action)
    if err != nil {
        return ActionEnvelope{}, err
    }
    return ActionEnvelope{Type: action.ActionType(), Raw: raw}, nil
}
```

### 1.4 ExecutorResult Interface

```go
// internal/head/agent/result.go — new file

type ExecutorResult interface {
    Success() bool
    Duration() time.Duration
    Summary() string
    Evidence() EvidenceData
}

type EvidenceData struct {
    Type     string `json:"type"`
    Content  string `json:"content"`
    Encoding string `json:"encoding,omitempty"` // "text" (default), "base64" for binary payloads
}

// truncate limits s to maxRunes, appending "..." if truncated.
func truncate(s string, maxRunes int) string {
    runes := []rune(s)
    if len(runes) <= maxRunes {
        return s
    }
    return string(runes[:maxRunes]) + "..."
}

// --- HTTP Result ---

type HTTPResult struct {
    OK         bool              `json:"success"`
    StatusCode int               `json:"status_code"`
    Body       string            `json:"body"`
    Headers    map[string]string `json:"headers"`
    URL        string            `json:"url"`
    Latency    time.Duration     `json:"duration"`
    Err        string            `json:"error,omitempty"`
}

func (r HTTPResult) Success() bool            { return r.OK }
func (r HTTPResult) Duration() time.Duration  { return r.Latency }
func (r HTTPResult) Summary() string {
    return fmt.Sprintf("HTTP %d %s (%s)", r.StatusCode, r.URL, r.Latency)
}
func (r HTTPResult) Evidence() EvidenceData {
    return EvidenceData{Type: "http_response", Content: r.Body}
}

// --- Process Result ---

type ProcessResult struct {
    OK       bool          `json:"success"`
    ExitCode int           `json:"exit_code"`
    Stdout   string        `json:"stdout"`
    Stderr   string        `json:"stderr"`
    Latency  time.Duration `json:"duration"`
    Err      string        `json:"error,omitempty"`
}

func (r ProcessResult) Success() bool            { return r.OK }
func (r ProcessResult) Duration() time.Duration  { return r.Latency }
func (r ProcessResult) Summary() string {
    return fmt.Sprintf("exit %d (%s)\nstdout: %s", r.ExitCode, r.Latency, truncate(r.Stdout, 500))
}
func (r ProcessResult) Evidence() EvidenceData {
    return EvidenceData{Type: "process_output", Content: r.Stdout}
}

// --- File Result ---

type FileResult struct {
    OK      bool          `json:"success"`
    Path    string        `json:"path"`
    Content string        `json:"content,omitempty"`
    Exists  bool          `json:"exists,omitempty"`
    Matches []string      `json:"matches,omitempty"`
    Latency time.Duration `json:"duration"`
    Err     string        `json:"error,omitempty"`
}

func (r FileResult) Success() bool            { return r.OK }
func (r FileResult) Duration() time.Duration  { return r.Latency }
func (r FileResult) Summary() string {
    if r.Err != "" {
        return fmt.Sprintf("file %s: %s", r.Path, r.Err)
    }
    return fmt.Sprintf("file %s OK (%s)", r.Path, r.Latency)
}
func (r FileResult) Evidence() EvidenceData {
    return EvidenceData{Type: "file_content", Content: r.Content}
}

// --- MCP Result ---

type MCPResult struct {
    OK      bool          `json:"success"`
    Body    string        `json:"body"`
    Latency time.Duration `json:"duration"`
    Err     string        `json:"error,omitempty"`
}

func (r MCPResult) Success() bool            { return r.OK }
func (r MCPResult) Duration() time.Duration  { return r.Latency }
func (r MCPResult) Summary() string {
    status := "error"
    if r.OK {
        status = "ok"
    }
    return fmt.Sprintf("MCP %s (%s)", status, r.Latency)
}
func (r MCPResult) Evidence() EvidenceData {
    return EvidenceData{Type: "mcp_response", Content: r.Body}
}

// --- Code Result ---

type CodeResult struct {
    OK       bool           `json:"success"`
    Findings []CodeFinding  `json:"findings"`
    Stats    CodeStats      `json:"stats"`
    Latency  time.Duration  `json:"duration"`
    Err      string         `json:"error,omitempty"`
}

type CodeFinding struct {
    File     string `json:"file"`
    Line     int    `json:"line"`
    Rule     string `json:"rule"`
    Message  string `json:"message"`
    Severity string `json:"severity"`
}

type CodeStats struct {
    FilesAnalyzed int     `json:"files_analyzed"`
    SymbolCount   int     `json:"symbol_count"`
    Coverage      float64 `json:"coverage,omitempty"`
}

func (r CodeResult) Success() bool            { return r.OK }
func (r CodeResult) Duration() time.Duration  { return r.Latency }
func (r CodeResult) Summary() string {
    return fmt.Sprintf("%d findings in %d files (%s)", len(r.Findings), r.Stats.FilesAnalyzed, r.Latency)
}
func (r CodeResult) Evidence() EvidenceData {
    content, _ := json.Marshal(r.Findings)
    return EvidenceData{Type: "code_findings", Content: string(content)}
}

// --- Error Result (generic) ---

type ErrorResult struct {
    Err     string        `json:"error"`
    Latency time.Duration `json:"duration,omitempty"`
}

func (r ErrorResult) Success() bool            { return false }
func (r ErrorResult) Duration() time.Duration  { return r.Latency }
func (r ErrorResult) Summary() string          { return fmt.Sprintf("error: %s", r.Err) }
func (r ErrorResult) Evidence() EvidenceData {
    return EvidenceData{Type: "error", Content: r.Err}
}
```

---

## Section 2: Executor Architecture

### 2.1 ActionExecutor Interface

```go
// internal/head/agent/executor.go — rewritten

type ActionExecutor interface {
    Execute(ctx context.Context, action TypedAction) ExecutorResult
}
```

### 2.2 MultiExecutor

```go
// internal/head/agent/multi.go — new file

type MultiExecutor struct {
    executors map[ActionType]ActionExecutor
    policy    policy.ActionPolicy
    sandbox   sandbox.Sandbox
    gate      escalation.Gate
    anomaly   *policy.AnomalyDetector
    logger    *zap.Logger
}

func NewMultiExecutor(
    p policy.ActionPolicy,
    sb sandbox.Sandbox,
    gate escalation.Gate,
    logger *zap.Logger,
) *MultiExecutor {
    return &MultiExecutor{
        executors: make(map[ActionType]ActionExecutor),
        policy:    p,
        sandbox:   sb,
        gate:      gate,
        anomaly:   policy.NewDefaultAnomalyDetector(),
        logger:    logger,
    }
}

func (m *MultiExecutor) Register(executor ActionExecutor, types ...ActionType) {
    for _, t := range types {
        m.executors[t] = executor
        m.logger.Info("registered executor", zap.String("action_type", string(t)))
    }
}

func (m *MultiExecutor) Execute(ctx context.Context, action TypedAction) ExecutorResult {
    // Layer 1: Policy validation
    if err := m.policy.Validate(action); err != nil {
        return ErrorResult{Err: fmt.Sprintf("policy denied: %v", err)}
    }

    // Layer 2: Sandbox isolation
    sbPolicy := m.sandboxPolicyFor(action)
    ctx, cleanup, err := m.sandbox.Apply(ctx, sbPolicy)
    if err != nil {
        return ErrorResult{Err: fmt.Sprintf("sandbox apply: %v", err)}
    }
    defer cleanup()

    // Layer 3: Route to executor
    executor, ok := m.executors[action.ActionType()]
    if !ok {
        return ErrorResult{Err: fmt.Sprintf("no executor for action type: %s", action.ActionType())}
    }
    result := executor.Execute(ctx, action)

    // Layer 4: Anomaly detection
    if m.anomaly.Check(result) {
        m.gate.Check(ctx, escalation.Event{
            Type:    "anomalous_result",
            Message: result.Summary(),
            Data:    map[string]any{"action_type": string(action.ActionType())},
        })
    }

    return result
}

func (m *MultiExecutor) sandboxPolicyFor(action TypedAction) sandbox.Policy {
    switch action.ActionType() {
    case ActionProcessExec, ActionProcessBuild:
        a := action.(ProcessExecAction)
        return sandbox.DefaultProcessPolicy(a.WorkDir)
    case ActionFileRead, ActionFileWrite, ActionFileExists, ActionFileGlob:
        return sandbox.DefaultFilePolicy(".")
    case ActionMCPCall:
        return sandbox.DefaultMCPPolicy()
    case ActionCodeAnalyze, ActionCodeLint, ActionCodeSymbols:
        return sandbox.DefaultCodePolicy(".")
    default:
        return sandbox.DefaultHTTPPolicy()
    }
}
```

### 2.3 Assembly

```go
func buildExecutor(cfg *config.Config, gate escalation.Gate, logger *zap.Logger) *MultiExecutor {
    p := policy.NewDefaultActionPolicy(".")
    sb := sandbox.NewLinuxSandbox(logger)
    multi := NewMultiExecutor(p, sb, gate, logger)

    multi.Register(NewHTTPExecutor(logger), ActionAPIRequest, ActionNavigate)
    multi.Register(NewProcessExecutor(sb, logger), ActionProcessExec, ActionProcessBuild)
    multi.Register(NewFileExecutor(sb, ".", logger), ActionFileRead, ActionFileWrite, ActionFileExists, ActionFileGlob)
    multi.Register(NewMCPExecutor(nil, logger), ActionMCPCall)
    multi.Register(NewCodeExecutor(sb, logger), ActionCodeAnalyze, ActionCodeLint, ActionCodeSymbols)
    multi.Register(NewWaitExecutor(), ActionWait)

    return multi
}
```

### 2.4 ReActLoop Adaptation

The ReActLoop must be rewritten to use `TypedAction` + `ExecutorResult` instead of `Action` + `Observation`. This section covers all required changes in `internal/head/agent/executor.go`.

**SteerOutput and RecoverOutput use ActionEnvelope:**

```go
// SteerOutput now returns an ActionEnvelope instead of Action.
type SteerOutput struct {
    Envelope ActionEnvelope `json:"action"`
    Reason   string         `json:"reason"`
}

// RecoverOutput similarly uses ActionEnvelope.
type RecoverOutput struct {
    Envelope ActionEnvelope `json:"action,omitempty"`
    Skip     bool           `json:"skip,omitempty"`
    Reason   string         `json:"reason,omitempty"`
}
```

**ReActLoop struct uses MultiExecutor:**

```go
type ReActLoop struct {
    driver   *ai.Driver
    store    *store.Store
    engine   *RuleEngine
    executor ActionExecutor  // unchanged interface, now a *MultiExecutor
    recovery recoverer
    config   ReActConfig
    logger   *zap.Logger
    gate     escalation.Gate
}
```

**steer() returns TypedAction via UnmarshalAction:**

```go
func (r *ReActLoop) steer(ctx context.Context, tc *TestCase, prevResult ExecutorResult, attempt int) (TypedAction, error) {
    observationCtx := formatResultContext(tc, prevResult, attempt)

    prompt := ai.NewPrompt().
        System(promptSteerSystem).
        Context(observationCtx).
        Task(fmt.Sprintf("Test case: %s\nTarget: %s\nExpectation: %s\nAttempt: %d/%d",
            tc.Name, tc.Target, tc.Expectation, attempt, r.config.MaxSteerAttempts)).
        Output(promptSteerOutput). // Updated: requests ActionEnvelope JSON
        Build()

    var out SteerOutput
    if err := r.driver.Decide(ctx, prompt, &out); err != nil {
        if isParseError(err) {
            r.logger.Warn("steer parse failed, using fallback", zap.Error(err))
            return FallbackParseAction(err.Error(), tc.Target)
        }
        return nil, fmt.Errorf("steer attempt %d: %w", attempt, err)
    }

    action, err := UnmarshalAction(out.Envelope)
    if err != nil {
        return nil, fmt.Errorf("unmarshal steer action: %w", err)
    }
    return action, nil
}
```

**Destructive action detection adapts to TypedAction:**

```go
// isDestructiveAction checks if a TypedAction is potentially destructive.
func isDestructiveAction(action TypedAction) bool {
    switch a := action.(type) {
    case HTTPAction:
        upper := strings.ToUpper(a.Method)
        return upper == "DELETE" || upper == "DROP"
    case ProcessExecAction:
        destructive := []string{"rm", "rmdir", "dropdb", "truncate"}
        for _, d := range destructive {
            if a.Command == d {
                return true
            }
        }
    case FileWriteAction:
        return true // overwriting files is flagged by default
    }
    return false
}
```

**Recovery path uses ActionEnvelope:**

```go
// In executeStep, recovery result is unmarshaled:
recResult, recErr := r.recovery.Recover(ctx, *tc, lastResult, attempt)
if recErr != nil {
    r.logger.Warn("recovery failed", zap.Error(recErr))
}
if recResult.Skip {
    recoverySkipped = true
    break
}
if recResult.Envelope.Type != "" {
    recAction, err := UnmarshalAction(recResult.Envelope)
    if err != nil {
        r.logger.Warn("unmarshal recovery action", zap.Error(err))
    } else {
        recResultExec := r.executor.Execute(ctx, recAction) // returns ExecutorResult
        r.recordEvidence(ctx, traceID, "recovery", recAction, recResultExec)
        lastResult = recResultExec
        if recResultExec.Success() {
            // recovery succeeded
            return StepResult{...}
        }
    }
}
```

**Result context formatting:**

```go
func formatResultContext(tc *TestCase, result ExecutorResult, attempt int) string {
    if attempt == 1 {
        return fmt.Sprintf("Target: %s", tc.Target)
    }
    return fmt.Sprintf("Target: %s\nPrevious: %s", tc.Target, result.Summary())
}
```

**Evidence recording adapts to ExecutorResult:**

```go
func (r *ReActLoop) recordEvidence(ctx context.Context, traceID int64, phase string, action TypedAction, result ExecutorResult) {
    content, _ := json.Marshal(map[string]any{
        "phase":    phase,
        "type":     string(action.ActionType()),
        "target":   action.Target(),
        "success":  result.Success(),
        "summary":  result.Summary(),
        "evidence": result.Evidence(),
    })
    _, err := r.store.CreateEvidence(ctx, traceID, "agent_observation", string(content))
    if err != nil {
        r.logger.Warn("record evidence", zap.Error(err))
    }
}
```

**LLM prompt template change:**

The `promptSteerOutput` template must request ActionEnvelope format:

```
Respond with JSON:
{
  "action": {
    "type": "<action_type>",
    "raw": { <action-specific fields> }
  },
  "reason": "<why this action>"
}
```

---

## Section 3: Sandbox

### 3.1 Sandbox Interface

```go
// internal/sandbox/sandbox.go — new package

type Sandbox interface {
    Apply(ctx context.Context, policy Policy) (context.Context, func(), error)
}

type Policy struct {
    FS        FSPolicy
    Network   NetPolicy
    Resources ResPolicy
}

type FSPolicy struct {
    ReadOnly  []string
    ReadWrite []string
    Denied    []string
}

type NetPolicy struct {
    AllowOutbound bool
    AllowHosts    []string
}

type ResPolicy struct {
    MaxMemoryMB   int
    MaxCPUPercent int
    Timeout       time.Duration
}
```

### 3.2 Linux Implementation (criyle/go-sandbox)

```go
// internal/sandbox/linux.go
//go:build linux

import (
    criylesandbox "github.com/criyle/go-sandbox"
)

type LinuxSandbox struct {
    manager *criylesandbox.Manager
    logger  *zap.Logger
}

func NewLinuxSandbox(logger *zap.Logger) *LinuxSandbox { ... }

func (s *LinuxSandbox) Apply(ctx context.Context, policy Policy) (context.Context, func(), error) {
    // Use criylesandbox to create isolated container.
    // Configure namespace + seccomp + cgroup based on Policy.
}
```

### 3.3 NoOp Implementation

```go
// internal/sandbox/noop.go

type NoOpSandbox struct{}

func (s NoOpSandbox) Apply(ctx context.Context, _ Policy) (context.Context, func(), error) {
    return ctx, func(){}, nil
}
```

### 3.4 Predefined Policies

```go
// internal/sandbox/policy.go

func DefaultProcessPolicy(workDir string) Policy {
    absWork, _ := filepath.Abs(workDir)
    return Policy{
        FS: FSPolicy{
            ReadOnly:  []string{"/usr", "/lib", "/go", "/tmp"},
            ReadWrite: []string{absWork},
            Denied:    []string{"/etc/shadow", "/root/.ssh", "/.env"},
        },
        Network: NetPolicy{AllowOutbound: false},
        Resources: ResPolicy{
            MaxMemoryMB: 512,
            Timeout:     60 * time.Second,
        },
    }
}

func DefaultFilePolicy(projectDir string) Policy {
    abs, _ := filepath.Abs(projectDir)
    return Policy{
        FS: FSPolicy{ReadWrite: []string{abs}},
        Network: NetPolicy{AllowOutbound: false},
    }
}

func DefaultHTTPPolicy() Policy {
    return Policy{
        Network: NetPolicy{AllowOutbound: true},
        Resources: ResPolicy{Timeout: 30 * time.Second},
    }
}

func DefaultMCPPolicy() Policy {
    return Policy{
        Network: NetPolicy{AllowOutbound: true},
        Resources: ResPolicy{Timeout: 10 * time.Second},
    }
}

func DefaultCodePolicy(projectDir string) Policy {
    abs, _ := filepath.Abs(projectDir)
    return Policy{
        FS: FSPolicy{ReadOnly: []string{abs}},
        Network: NetPolicy{AllowOutbound: false},
    }
}
```

---

## Section 4: Executors

### 4.1 HTTPExecutor

Adapted from existing `HTTPActionExecutor`. Core HTTP logic unchanged, wrapped in new interface.

```go
// internal/head/agent/http.go — new file (replaces old action.go)

type HTTPExecutor struct {
    client *http.Client
    logger *zap.Logger
}

func NewHTTPExecutor(logger *zap.Logger) *HTTPExecutor {
    return &HTTPExecutor{
        client: &http.Client{Timeout: 30 * time.Second},
        logger: logger,
    }
}

func (e *HTTPExecutor) Execute(ctx context.Context, action TypedAction) ExecutorResult {
    start := time.Now()
    switch a := action.(type) {
    case HTTPAction:
        return e.doHTTP(ctx, a, start)
    case NavigateAction:
        return e.doHTTP(ctx, HTTPAction{Method: "GET", URL: a.URL}, start)
    default:
        return ErrorResult{Err: fmt.Sprintf("http executor: unsupported action %T", action)}
    }
}

func (e *HTTPExecutor) doHTTP(ctx context.Context, a HTTPAction, start time.Time) ExecutorResult {
    var body io.Reader
    if a.Body != "" {
        body = strings.NewReader(a.Body)
    }
    req, err := http.NewRequestWithContext(ctx, a.Method, a.URL, body)
    if err != nil {
        return HTTPResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
    }
    for k, v := range a.Headers {
        req.Header.Set(k, v)
    }
    resp, err := e.client.Do(req)
    if err != nil {
        return HTTPResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
    }
    defer resp.Body.Close()
    respBody, _ := io.ReadAll(resp.Body)
    headers := make(map[string]string)
    for k, v := range resp.Header {
        if len(v) > 0 {
            headers[k] = v[0]
        }
    }
    ok := resp.StatusCode >= 200 && resp.StatusCode < 400
    return HTTPResult{
        OK: ok, StatusCode: resp.StatusCode, Body: string(respBody),
        Headers: headers, URL: a.URL, Latency: time.Since(start),
    }
}
```

### 4.2 ProcessExecutor

Executes system commands with optional sandbox isolation. Validates command against allowlist before execution.

```go
// internal/head/agent/process.go — new file

type ProcessExecutor struct {
    sandbox sandbox.Sandbox
    logger  *zap.Logger
}

func NewProcessExecutor(sb sandbox.Sandbox, logger *zap.Logger) *ProcessExecutor {
    return &ProcessExecutor{sandbox: sb, logger: logger}
}

func (e *ProcessExecutor) Execute(ctx context.Context, action TypedAction) ExecutorResult {
    start := time.Now()
    a, ok := action.(ProcessExecAction)
    if !ok {
        return ErrorResult{Err: fmt.Sprintf("process executor: unsupported action %T", action)}
    }

    // Parse timeout (default 60s).
    timeout := 60 * time.Second
    if a.Timeout != "" {
        if d, err := time.ParseDuration(a.Timeout); err == nil {
            timeout = d
        }
    }
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    cmd := exec.CommandContext(ctx, a.Command, a.Args...)
    if a.WorkDir != "" {
        cmd.Dir = a.WorkDir
    }
    // Merge environment: inherit current + overlay.
    if len(a.Env) > 0 {
        cmd.Env = os.Environ()
        for k, v := range a.Env {
            cmd.Env = append(cmd.Env, k+"="+v)
        }
    }

    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    err := cmd.Run()
    exitCode := 0
    if err != nil {
        var exitErr *exec.ExitError
        if errors.As(err, &exitErr) {
            exitCode = exitErr.ExitCode()
        } else {
            return ProcessResult{
                OK: false, ExitCode: -1, Stderr: err.Error(),
                Latency: time.Since(start), Err: err.Error(),
            }
        }
    }

    ok = exitCode == 0
    return ProcessResult{
        OK: ok, ExitCode: exitCode,
        Stdout: stdout.String(), Stderr: stderr.String(),
        Latency: time.Since(start),
    }
}
```

### 4.3 FileExecutor

Read/write/exists/glob operations restricted to project directory. Path traversal prevention via `filepath.Rel` check.

```go
// internal/head/agent/file.go — new file

type FileExecutor struct {
    sandbox     sandbox.Sandbox
    projectRoot string
    logger      *zap.Logger
}

func NewFileExecutor(sb sandbox.Sandbox, projectRoot string, logger *zap.Logger) *FileExecutor {
    abs, _ := filepath.Abs(projectRoot)
    return &FileExecutor{sandbox: sb, projectRoot: abs, logger: logger}
}

// safePath resolves and validates that path stays within projectRoot.
func (e *FileExecutor) safePath(p string) (string, error) {
    abs, err := filepath.Abs(filepath.Join(e.projectRoot, p))
    if err != nil {
        return "", fmt.Errorf("resolve path: %w", err)
    }
    rel, err := filepath.Rel(e.projectRoot, abs)
    if err != nil {
        return "", fmt.Errorf("relative path: %w", err)
    }
    if strings.HasPrefix(rel, "..") {
        return "", fmt.Errorf("path escapes project root: %s", p)
    }
    return abs, nil
}

func (e *FileExecutor) Execute(ctx context.Context, action TypedAction) ExecutorResult {
    start := time.Now()
    switch a := action.(type) {
    case FileReadAction:
        return e.readFile(a, start)
    case FileWriteAction:
        return e.writeFile(a, start)
    case FileExistsAction:
        return e.existsFile(a, start)
    case FileGlobAction:
        return e.globFiles(a, start)
    default:
        return ErrorResult{Err: fmt.Sprintf("file executor: unsupported action %T", action)}
    }
}

func (e *FileExecutor) readFile(a FileReadAction, start time.Time) ExecutorResult {
    path, err := e.safePath(a.Path)
    if err != nil {
        return FileResult{OK: false, Path: a.Path, Err: err.Error(), Latency: time.Since(start)}
    }
    data, err := os.ReadFile(path)
    if err != nil {
        return FileResult{OK: false, Path: a.Path, Err: err.Error(), Latency: time.Since(start)}
    }
    return FileResult{OK: true, Path: path, Content: string(data), Latency: time.Since(start)}
}

func (e *FileExecutor) writeFile(a FileWriteAction, start time.Time) ExecutorResult {
    path, err := e.safePath(a.Path)
    if err != nil {
        return FileResult{OK: false, Path: a.Path, Err: err.Error(), Latency: time.Since(start)}
    }
    if err := os.WriteFile(path, []byte(a.Content), 0644); err != nil {
        return FileResult{OK: false, Path: a.Path, Err: err.Error(), Latency: time.Since(start)}
    }
    return FileResult{OK: true, Path: path, Latency: time.Since(start)}
}

func (e *FileExecutor) existsFile(a FileExistsAction, start time.Time) ExecutorResult {
    path, err := e.safePath(a.Path)
    if err != nil {
        return FileResult{OK: false, Path: a.Path, Err: err.Error(), Latency: time.Since(start)}
    }
    _, statErr := os.Stat(path)
    exists := statErr == nil
    return FileResult{OK: true, Path: path, Exists: exists, Latency: time.Since(start)}
}

func (e *FileExecutor) globFiles(a FileGlobAction, start time.Time) ExecutorResult {
    pattern := filepath.Join(e.projectRoot, a.Pattern)
    matches, err := filepath.Glob(pattern)
    if err != nil {
        return FileResult{OK: false, Path: a.Pattern, Err: err.Error(), Latency: time.Since(start)}
    }
    return FileResult{OK: true, Path: a.Pattern, Matches: matches, Latency: time.Since(start)}
}
```

### 4.4 MCPExecutor

JSON-RPC 2.0 client for calling MCP servers. Configured with named endpoints read from project config.

```go
// internal/head/agent/mcp_exec.go — new file

type MCPEndpoint struct {
    Name    string
    Address string // e.g. "stdio" or "localhost:8080"
}

type MCPExecutor struct {
    endpoints map[string]MCPEndpoint
    logger    *zap.Logger
}

func NewMCPExecutor(endpoints map[string]MCPEndpoint, logger *zap.Logger) *MCPExecutor {
    if endpoints == nil {
        endpoints = make(map[string]MCPEndpoint)
    }
    return &MCPExecutor{endpoints: endpoints, logger: logger}
}

func (e *MCPExecutor) Execute(ctx context.Context, action TypedAction) ExecutorResult {
    start := time.Now()
    a, ok := action.(MCPCallAction)
    if !ok {
        return ErrorResult{Err: fmt.Sprintf("mcp executor: unsupported action %T", action)}
    }

    ep, found := e.endpoints[a.Server]
    if !found {
        return MCPResult{OK: false, Err: fmt.Sprintf("unknown MCP server: %s", a.Server), Latency: time.Since(start)}
    }

    // Build JSON-RPC request.
    req := map[string]any{
        "jsonrpc": "2.0",
        "id":      time.Now().UnixNano(),
        "method":  a.Method,
        "params":  a.Params,
    }
    reqBody, _ := json.Marshal(req)

    // Send to endpoint (stdio or TCP).
    respBody, err := e.send(ctx, ep, reqBody)
    if err != nil {
        return MCPResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
    }

    var resp struct {
        Result any    `json:"result"`
        Error  *struct {
            Message string `json:"message"`
        } `json:"error"`
    }
    if err := json.Unmarshal(respBody, &resp); err != nil {
        return MCPResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
    }
    if resp.Error != nil {
        return MCPResult{OK: false, Err: resp.Error.Message, Latency: time.Since(start)}
    }

    resultJSON, _ := json.Marshal(resp.Result)
    return MCPResult{OK: true, Body: string(resultJSON), Latency: time.Since(start)}
}

// send dispatches to stdio or TCP based on endpoint address.
func (e *MCPExecutor) send(ctx context.Context, ep MCPEndpoint, body []byte) ([]byte, error) {
    if ep.Address == "stdio" {
        // Start process, write to stdin, read from stdout.
        // Full implementation handles process lifecycle.
        return nil, fmt.Errorf("stdio MCP not yet implemented")
    }
    // TCP: dial, write, read.
    var d net.Dialer
    conn, err := d.DialContext(ctx, "tcp", ep.Address)
    if err != nil {
        return nil, err
    }
    defer conn.Close()
    conn.SetDeadline(time.Now().Add(10 * time.Second))
    body = append(body, '\n')
    if _, err := conn.Write(body); err != nil {
        return nil, err
    }
    reader := bufio.NewReader(conn)
    return reader.ReadBytes('\n')
}
```

### 4.5 CodeExecutor

Pure Go static analysis using `go/parser` + `go/ast`. Built-in checks: dead code, cyclomatic complexity, unhandled errors. No external dependencies.

```go
// internal/head/agent/code.go — new file

type CodeExecutor struct {
    sandbox sandbox.Sandbox
    logger  *zap.Logger
}

func NewCodeExecutor(sb sandbox.Sandbox, logger *zap.Logger) *CodeExecutor {
    return &CodeExecutor{sandbox: sb, logger: logger}
}

func (e *CodeExecutor) Execute(ctx context.Context, action TypedAction) ExecutorResult {
    start := time.Now()
    switch a := action.(type) {
    case CodeAnalyzeAction:
        return e.analyze(a, start)
    case CodeLintAction:
        return e.lint(a, start)
    case CodeSymbolsAction:
        return e.symbols(a, start)
    default:
        return ErrorResult{Err: fmt.Sprintf("code executor: unsupported action %T", action)}
    }
}

func (e *CodeExecutor) analyze(a CodeAnalyzeAction, start time.Time) ExecutorResult {
    findings, stats, err := e.parseAndAnalyze(a.TargetPath, a.Checks)
    if err != nil {
        return CodeResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
    }
    return CodeResult{
        OK:       len(findings) == 0, // no findings = success
        Findings: findings,
        Stats:    stats,
        Latency:  time.Since(start),
    }
}

func (e *CodeExecutor) lint(a CodeLintAction, start time.Time) ExecutorResult {
    checks := a.Rules
    if len(checks) == 0 {
        checks = []string{"unhandled_error", "golint"}
    }
    findings, stats, err := e.parseAndAnalyze(a.TargetPath, checks)
    if err != nil {
        return CodeResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
    }
    return CodeResult{OK: len(findings) == 0, Findings: findings, Stats: stats, Latency: time.Since(start)}
}

func (e *CodeExecutor) symbols(a CodeSymbolsAction, start time.Time) ExecutorResult {
    fset := token.NewFileSet()
    pkgs, err := parser.ParseDir(fset, a.TargetPath, nil, parser.ImportsOnly)
    if err != nil {
        return CodeResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
    }
    var count int
    for _, pkg := range pkgs {
        count += len(pkg.Files)
        for _, f := range pkg.Files {
            count += len(f.Decls)
        }
    }
    return CodeResult{
        OK: true,
        Stats: CodeStats{FilesAnalyzed: len(pkgs), SymbolCount: count},
        Latency: time.Since(start),
    }
}

// parseAndAnalyze walks Go source files and runs check functions.
func (e *CodeExecutor) parseAndAnalyze(root string, checks []string) ([]CodeFinding, CodeStats, error) {
    fset := token.NewFileSet()
    pkgs, err := parser.ParseDir(fset, root, nil, parser.AllErrors|parser.ParseComments)
    if err != nil {
        return nil, CodeStats{}, err
    }

    if len(checks) == 0 {
        checks = []string{"dead_code", "complexity", "unhandled_error"}
    }
    checkFns := map[string]func(*ast.File, *token.FileSet) []CodeFinding{
        "dead_code":       checkDeadCode,
        "complexity":      checkComplexity,
        "unhandled_error": checkUnhandledErrors,
    }

    var allFindings []CodeFinding
    fileCount := 0
    for _, pkg := range pkgs {
        for path, f := range pkg.Files {
            fileCount++
            for _, check := range checks {
                if fn, ok := checkFns[check]; ok {
                    findings := fn(f, fset)
                    for i := range findings {
                        findings[i].File = path
                    }
                    allFindings = append(allFindings, findings...)
                }
            }
        }
    }
    return allFindings, CodeStats{FilesAnalyzed: fileCount, SymbolCount: len(allFindings)}, nil
}

// checkDeadCode flags unused package-level declarations.
func checkDeadCode(f *ast.File, fset *token.FileSet) []CodeFinding {
    // Scan for unexported, unreferenced top-level declarations.
    // Full version builds a reference graph.
    return nil
}

// checkComplexity flags functions with cyclomatic complexity > 15.
func checkComplexity(f *ast.File, fset *token.FileSet) []CodeFinding {
    var findings []CodeFinding
    for _, decl := range f.Decls {
        fn, ok := decl.(*ast.FuncDecl)
        if !ok {
            continue
        }
        complexity := calcComplexity(fn.Body)
        if complexity > 15 {
            pos := fset.Position(fn.Pos())
            findings = append(findings, CodeFinding{
                Line:     pos.Line,
                Rule:     "high_complexity",
                Message:  fmt.Sprintf("function %s has complexity %d (threshold: 15)", fn.Name.Name, complexity),
                Severity: "warning",
            })
        }
    }
    return findings
}

// checkUnhandledErrors flags assignments without error checks.
func checkUnhandledErrors(f *ast.File, fset *token.FileSet) []CodeFinding {
    var findings []CodeFinding
    ast.Inspect(f, func(n ast.Node) bool {
        assign, ok := n.(*ast.AssignStmt)
        if !ok {
            return true
        }
        // Check if last RHS is a call and error return is not captured.
        if len(assign.Rhs) > 0 {
            if _, isCall := assign.Rhs[len(assign.Rhs)-1].(*ast.CallExpr); isCall {
                // Full version resolves return types via go/types.
                // Simplified: flag if fewer LHS than expected.
            }
        }
        return true
    })
    return findings
}

func calcComplexity(block *ast.BlockStmt) int {
    c := 1 // base
    ast.Inspect(block, func(n ast.Node) bool {
        switch n.(type) {
        case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.CaseClause:
            c++
        case *ast.BinaryExpr:
            // && and || add to complexity (checked via Op)
        }
        return true
    })
    return c
}
```

### 4.6 WaitExecutor

Simple duration-based pause. No sandbox needed.

```go
// internal/head/agent/wait.go — new file

type WaitExecutor struct{}

func NewWaitExecutor() *WaitExecutor { return &WaitExecutor{} }

func (e *WaitExecutor) Execute(ctx context.Context, action TypedAction) ExecutorResult {
    start := time.Now()
    a, ok := action.(WaitAction)
    if !ok {
        return ErrorResult{Err: fmt.Sprintf("wait executor: unsupported action %T", action)}
    }
    if a.Duration == "" {
        return ErrorResult{Err: "wait: duration is required"}
    }
    d, err := time.ParseDuration(a.Duration)
    if err != nil {
        return ErrorResult{Err: fmt.Sprintf("wait: invalid duration %q: %v", a.Duration, err)}
    }
    select {
    case <-time.After(d):
        return ProcessResult{OK: true, Latency: time.Since(start)}
    case <-ctx.Done():
        return ErrorResult{Err: ctx.Err().Error(), Latency: time.Since(start)}
    }
}
```

### 4.7 ProcessManager

Generic background process lifecycle management. Start → health check → stop at session end. Used whenever a TestCase requires a running service.

```go
// internal/head/agent/process_mgr.go — new file

type ManagedProcess struct {
    Name    string
    Cmd     string
    Args    []string
    WorkDir string
    Health  string        // URL for health check
    Timeout time.Duration // max wait for health endpoint

    cmd     *exec.Cmd
    running bool
}

type ProcessManager struct {
    processes map[string]*ManagedProcess
    logger    *zap.Logger
}

func NewProcessManager(logger *zap.Logger) *ProcessManager {
    return &ProcessManager{processes: make(map[string]*ManagedProcess), logger: logger}
}

// Start launches a background process and waits for its health endpoint.
func (pm *ProcessManager) Start(ctx context.Context, mp *ManagedProcess) error {
    cmd := exec.CommandContext(ctx, mp.Cmd, mp.Args...)
    cmd.Dir = mp.WorkDir
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    if err := cmd.Start(); err != nil {
        return fmt.Errorf("start %s: %w", mp.Name, err)
    }
    mp.cmd = cmd
    mp.running = true
    pm.processes[mp.Name] = mp

    // Wait for health endpoint if specified.
    if mp.Health != "" {
        deadline := time.Now().Add(mp.Timeout)
        for time.Now().Before(deadline) {
            resp, err := http.Get(mp.Health)
            if err == nil && resp.StatusCode < 500 {
                resp.Body.Close()
                return nil
            }
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(500 * time.Millisecond):
            }
        }
        pm.Stop(mp.Name)
        return fmt.Errorf("health check failed for %s at %s", mp.Name, mp.Health)
    }
    return nil
}

// Stop terminates a managed process gracefully (SIGTERM, then SIGKILL after 5s).
func (pm *ProcessManager) Stop(name string) error {
    mp, ok := pm.processes[name]
    if !ok || !mp.running {
        return nil
    }
    if mp.cmd != nil && mp.cmd.Process != nil {
        mp.cmd.Process.Signal(syscall.SIGTERM)
        done := make(chan error, 1)
        go func() { done <- mp.cmd.Wait() }()
        select {
        case <-done:
        case <-time.After(5 * time.Second):
            mp.cmd.Process.Kill()
        }
    }
    mp.running = false
    return nil
}

// StopAll terminates all managed processes (called at session end).
func (pm *ProcessManager) StopAll() {
    for name := range pm.processes {
        pm.Stop(name)
    }
}
```

---

## Section 5: Scout Type-Aware Planning

### 5.1 Project Type Detection

```go
// internal/head/scout/project_detect.go — new file

type ProjectType string

const (
    ProjectTypeUnknown ProjectType = "unknown"
    ProjectTypeGo      ProjectType = "go"
    ProjectTypeNode    ProjectType = "node"
    ProjectTypePython  ProjectType = "python"
    ProjectTypeHTTP    ProjectType = "http"
)

type ProjectInfo struct {
    Type       ProjectType
    RootDir    string
    BuildCmd   string
    TestCmd    string
    LintCmd    string
    Entrypoint string
    Language   string
}

func DetectProjectType(rootDir string) ProjectInfo {
    info := ProjectInfo{Type: ProjectTypeUnknown, RootDir: rootDir}

    // Go: look for go.mod
    if _, err := os.Stat(filepath.Join(rootDir, "go.mod")); err == nil {
        info.Type = ProjectTypeGo
        info.Language = "go"
        info.BuildCmd = "go build ./..."
        info.TestCmd = "go test -v -race ./..."
        info.LintCmd = "golangci-lint run ./..."
        if matches, _ := filepath.Glob(filepath.Join(rootDir, "cmd", "*", "main.go")); len(matches) > 0 {
            info.Entrypoint = matches[0]
        }
        return info
    }

    // Node: look for package.json
    if data, err := os.ReadFile(filepath.Join(rootDir, "package.json")); err == nil {
        info.Type = ProjectTypeNode
        info.Language = "javascript"
        var pkg struct {
            Scripts map[string]string `json:"scripts"`
        }
        json.Unmarshal(data, &pkg)
        if v, ok := pkg.Scripts["build"]; ok { info.BuildCmd = "npm run " + v }
        if v, ok := pkg.Scripts["test"]; ok { info.TestCmd = "npm run " + v }
        if v, ok := pkg.Scripts["lint"]; ok { info.LintCmd = "npm run " + v }
        return info
    }

    // Python: look for pyproject.toml, setup.py, requirements.txt
    for _, f := range []string{"pyproject.toml", "setup.py", "requirements.txt"} {
        if _, err := os.Stat(filepath.Join(rootDir, f)); err == nil {
            info.Type = ProjectTypePython
            info.Language = "python"
            info.TestCmd = "pytest"
            info.LintCmd = "ruff check ."
            return info
        }
    }

    return info
}
```

### 5.2 GenerateExecutorCases

Produces non-HTTP test cases based on project type. For Go projects: build, test, static analysis, symbol inventory, file integrity checks.

```go
// internal/head/scout/plan_executor.go — new file

func GenerateExecutorCases(info ProjectInfo, goal string) []TestCase {
    var cases []TestCase

    switch info.Type {
    case ProjectTypeGo:
        cases = append(cases,
            TestCase{
                Name: "Build project", Target: info.RootDir,
                Method: string(ActionProcessExec),
                Expectation: "Build completes without errors",
            },
            TestCase{
                Name: "Run tests", Target: info.RootDir,
                Method: string(ActionProcessExec),
                Expectation: "All tests pass",
            },
            TestCase{
                Name: "Static analysis", Target: info.RootDir,
                Method: string(ActionCodeAnalyze),
                Expectation: "No critical findings",
            },
            TestCase{
                Name: "Symbol inventory", Target: info.RootDir,
                Method: string(ActionCodeSymbols),
                Expectation: "Package structure is valid",
            },
        )
    case ProjectTypeNode:
        cases = append(cases,
            TestCase{
                Name: "Install dependencies", Target: info.RootDir,
                Method: string(ActionProcessExec),
                Expectation: "npm install succeeds",
            },
            TestCase{
                Name: "Run tests", Target: info.RootDir,
                Method: string(ActionProcessExec),
                Expectation: "All tests pass",
            },
        )
    case ProjectTypePython:
        cases = append(cases,
            TestCase{
                Name: "Run tests", Target: info.RootDir,
                Method: string(ActionProcessExec),
                Expectation: "All tests pass",
            },
            TestCase{
                Name: "Lint", Target: info.RootDir,
                Method: string(ActionProcessExec),
                Expectation: "No lint errors",
            },
        )
    }

    return cases
}
```

### 5.3 Plan Integration

```go
// In scout.go, Plan method appends executor cases when project type is detected:

func (s *Scout) Plan(ctx context.Context, goal string, cfg *project.Config) (*TestPlan, error) {
    // ... existing HTTP plan generation ...

    // If a local directory is configured, detect project type and append executor cases.
    if cfg.ProjectDir != "" {
        info := DetectProjectType(cfg.ProjectDir)
        if info.Type != ProjectTypeUnknown && info.Type != ProjectTypeHTTP {
            executorCases := GenerateExecutorCases(info, goal)
            plan.Cases = append(plan.Cases, executorCases...)
        }
    }
    return plan, nil
}
```

---

## Section 6: Policy Engine

### 6.1 ActionPolicy Interface

```go
// internal/policy/policy.go — new package

// ActionPolicy validates actions before execution.
// Returns nil if the action is allowed, or an error describing why it was denied.
type ActionPolicy interface {
    Validate(action TypedAction) error
}
```

### 6.2 DefaultActionPolicy

```go
// internal/policy/default.go

type DefaultActionPolicy struct {
    projectRoot   string
    allowedCmds   map[string]bool
    deniedPaths   []string
    deniedEnvKeys []string
    allowedMCP    map[string]bool
}

func NewDefaultActionPolicy(projectRoot string) *DefaultActionPolicy {
    abs, _ := filepath.Abs(projectRoot)
    return &DefaultActionPolicy{
        projectRoot: abs,
        allowedCmds: map[string]bool{
            "go": true, "node": true, "npm": true, "npx": true,
            "python": true, "pytest": true, "ruff": true,
            "make": true, "cargo": true, "git": true,
            "golangci-lint": true, "gofmt": true, "goimports": true,
        },
        deniedPaths: []string{
            "/etc/shadow", "/etc/passwd", "/root/.ssh",
            "/.env", "/var/run/docker.sock",
        },
        deniedEnvKeys: []string{
            "HOME", "USER", "SUDO_USER", "SSH_AUTH_SOCK",
        },
        allowedMCP: map[string]bool{
            "tools/call": true, "tools/list": true,
            "resources/read": true,
        },
    }
}

func (p *DefaultActionPolicy) Validate(action TypedAction) error {
    switch a := action.(type) {
    case ProcessExecAction:
        // Command allowlist.
        if !p.allowedCmds[a.Command] {
            return fmt.Errorf("command not allowed: %s", a.Command)
        }
        // WorkDir containment.
        if a.WorkDir != "" {
            abs, _ := filepath.Abs(a.WorkDir)
            rel, err := filepath.Rel(p.projectRoot, abs)
            if err != nil || strings.HasPrefix(rel, "..") {
                return fmt.Errorf("workdir escapes project: %s", a.WorkDir)
            }
        }
        // Denied env keys.
        for k := range a.Env {
            for _, denied := range p.deniedEnvKeys {
                if strings.EqualFold(k, denied) {
                    return fmt.Errorf("env key denied: %s", k)
                }
            }
        }
        // Arg injection prevention (no shell metacharacters).
        for _, arg := range a.Args {
            if strings.ContainsAny(arg, "|&;`$()") {
                return fmt.Errorf("arg contains shell metacharacters: %s", arg)
            }
        }

    case FileWriteAction:
        abs, _ := filepath.Abs(a.Path)
        for _, denied := range p.deniedPaths {
            if abs == denied {
                return fmt.Errorf("path denied: %s", a.Path)
            }
        }
        rel, err := filepath.Rel(p.projectRoot, abs)
        if err != nil || strings.HasPrefix(rel, "..") {
            return fmt.Errorf("path escapes project: %s", a.Path)
        }

    case FileReadAction:
        abs, _ := filepath.Abs(a.Path)
        for _, denied := range p.deniedPaths {
            if abs == denied {
                return fmt.Errorf("path denied: %s", a.Path)
            }
        }

    case FileExistsAction:
        abs, _ := filepath.Abs(a.Path)
        for _, denied := range p.deniedPaths {
            if abs == denied {
                return fmt.Errorf("path denied: %s", a.Path)
            }
        }

    case MCPCallAction:
        if !p.allowedMCP[a.Method] {
            return fmt.Errorf("MCP method not allowed: %s", a.Method)
        }

    case CodeAnalyzeAction, CodeLintAction, CodeSymbolsAction:
        abs, _ := filepath.Abs(a.Target())
        rel, err := filepath.Rel(p.projectRoot, abs)
        if err != nil || strings.HasPrefix(rel, "..") {
            return fmt.Errorf("target path escapes project: %s", a.Target())
        }
    }
    return nil
}
```

### 6.3 Configurable Overrides

`.cerberus/policy.yaml` allows projects to add/remove allowed commands, deny paths, adjust limits.

```yaml
# .cerberus/policy.yaml
commands:
  allow:
    - docker
    - kubectl
  deny:
    - curl   # override default allow
paths:
  deny:
    - /secrets
    - /root/data
mcp:
  allow_methods:
    - tools/call
    - prompts/get
limits:
  max_process_timeout: 120s
  max_output_bytes: 1048576
```

Policy loads as overlay on top of defaults: `NewDefaultActionPolicy` + YAML merge.

### 6.4 AnomalyDetector

Post-execution pattern detection: excessive output, permission escalation attempts, sensitive file content access.

```go
// internal/policy/anomaly.go

type AnomalyDetector struct {
    maxOutputBytes    int
    sensitivePatterns []*regexp.Regexp
}

func NewDefaultAnomalyDetector() *AnomalyDetector {
    return &AnomalyDetector{
        maxOutputBytes: 1 << 20, // 1MB
        sensitivePatterns: []*regexp.Regexp{
            regexp.MustCompile(`(?i)password\s*[:=]`),
            regexp.MustCompile(`(?i)secret\s*[:=]`),
            regexp.MustCompile(`(?i)api[_-]?key\s*[:=]`),
            regexp.MustCompile(`-----BEGIN (RSA |EC )?PRIVATE KEY-----`),
        },
    }
}

func (d *AnomalyDetector) Check(result ExecutorResult) bool {
    evidence := result.Evidence()

    // Check for excessive output.
    if len(evidence.Content) > d.maxOutputBytes {
        return true
    }

    // Check for sensitive content in evidence.
    for _, p := range d.sensitivePatterns {
        if p.MatchString(evidence.Content) {
            return true
        }
    }

    return false
}
```

---

## Section 7: General-Purpose Local Project Testing

### 7.1 CLI Extension

`cerberus run --dir <path>` enables local project testing. Combined with `--url` for full-stack testing.

```bash
# Test any Go project
cerberus run --dir ./my-project --goal "Build and test"

# Test any web project (process + HTTP)
cerberus run --dir ./my-app --url http://localhost:3000 --goal "Full stack test"

# Test remote API only (no local code)
cerberus run --url https://api.example.com --goal "Test endpoints"
```

### 7.2 TestCase Extensions

```go
type TestCase struct {
    // ... existing fields
    Background bool   `json:"background,omitempty"` // run in background via ProcessManager
    WaitFor    string `json:"wait_for,omitempty"`   // health check URL (e.g. "http://localhost:3000/health")
    Cleanup    bool   `json:"cleanup,omitempty"`    // stop process at session end
}
```

Background TestCase flow:
1. ProcessManager starts the process
2. Waits for `WaitFor` health endpoint to respond
3. Session tracks the process; `Cleanup=true` ensures `ProcessManager.StopAll()` at session end
4. Subsequent test cases in the plan can use HTTP actions against the now-running service

---

## Section 8: Migration Strategy

### Phase 1: Base Types (non-breaking)

Add new files: `actions.go`, `result.go`, `sandbox/`, `policy/`. No changes to existing code.

### Phase 2: Executor Layer (parallel coexistence)

Add `multi.go`, all executor implementations, `process_mgr.go`. Legacy `HTTPActionExecutor` wrapped via temporary adapter. Existing `ReActLoop` unchanged.

### Phase 3: ReActLoop Switch (breaking)

Rewrite `executor.go` to use `TypedAction` + `ExecutorResult` (see Section 2.4 for complete changes). Delete old `action.go` and adapter. Update `session.go`, `mcp/server.go`, `server.go`, `main.go`.

Key files changed in Phase 3:
- `internal/head/agent/executor.go` — ReActLoop uses MultiExecutor, SteerOutput/RecoverOutput use ActionEnvelope
- `internal/head/agent/types.go` — TestCase gains Background/WaitFor/Cleanup fields
- `internal/session/session.go` — wire MultiExecutor instead of HTTPActionExecutor
- `internal/mcp/server.go` — handleRun passes project dir to session
- `cmd/cerberus/main.go` — add `--dir` flag

### Phase 4: Scout + CLI

Add `project_detect.go`, `plan_executor.go`. Extend `scout.go` for type-aware planning. Add `--dir` flag to `runCmd`.

### Dependency Additions

```
github.com/criyle/go-sandbox    — Linux sandbox (namespace + seccomp + cgroup)
github.com/elastic/go-seccomp-bpf — Pure Go seccomp fallback
```

### File Change Summary

| Operation | Count | Files |
|-----------|-------|-------|
| New       | ~20   | actions.go, result.go, multi.go, 6 executors, sandbox/ (4 files), policy/ (4 files), project_detect.go, process_mgr.go |
| Modified  | ~10   | executor.go, types.go, session.go, mcp/server.go, server.go, scout.go, main.go |
| Deleted   | ~2    | action.go (replaced by http.go), adapter.go (temporary) |

---

## Design Decisions Log

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Capability scope | All 5 executors + code analysis | User wants comprehensive coverage |
| Routing mechanism | Plugin registry (map[ActionType]ActionExecutor) | Go idiom, extensible, O(1) lookup |
| Result type | Interface-based ExecutorResult | Semantic precision over backward compatibility |
| Action representation | Sum type (ActionEnvelope + TypedAction) | Type-safe per-executor actions, clean JSON serialization |
| Sandbox strategy | Layered: Policy + Sandbox + Escalation | Defense-in-depth, Sandbox is core not optional |
| Sandbox implementation | criyle/go-sandbox (accepts CGo) | Most mature, proven in production OJ scenarios |
| Isolation | Goroutine + Context | Single binary, no container dependency |
| Scout extension | Type-aware planning | Auto-detect project type, generate matching test cases |
| Self-testing | No special mechanism | `--dir .` is same as testing any project |
| Migration | 4-phase incremental | Phases 1-2 non-breaking, Phase 3 one-time switch |

---

## Implementation Status (updated 2026-06-12)

Commits: `4c12720` (core framework) → `9c3ea29` (completion) → `a5bbe10` (follow-up) → `db40d35` (test+integration)

### Completed

- [x] Core type system (ActionType, TypedAction, ActionEnvelope, ExecutorResult)
- [x] MultiExecutor plugin registry with O(1) dispatch
- [x] 6 executor implementations (HTTP, Process, File, MCP, Code, Wait)
- [x] ReActLoop rewrite using TypedAction/ExecutorResult
- [x] Sandbox interface + NoOpSandbox + LinuxSandbox (criyle/go-sandbox)
- [x] Policy engine (ActionPolicy, AnomalyDetector)
- [x] Scout type-aware planning (DetectProjectType, GenerateExecutorCases)
- [x] RuleEngine 14 rules (3 HTTP + 11 non-HTTP)
- [x] ProcessManager (background process lifecycle with health polling)
- [x] MCP stdio transport (subprocess JSON-RPC over stdin/stdout)
- [x] LinuxSandbox auto-detect + graceful fallback
- [x] TestCase Background/WaitFor/Cleanup fields
- [x] Escalation checkpoints (budget, systemic failure, destructive, unreachable)
- [x] ~30 new tests across all new components, all pass with -race
- [x] CodeExecutor: checkUnhandledErrors (two-phase AST) + checkDeadCode (package-level) — `a5bbe10`
- [x] Policy YAML runtime overrides (.cerberus/policy.yaml) — `a5bbe10`
- [x] --dir local project testing mode (--url optional) — `a5bbe10`
- [x] LLM clients: injectable httpClient/serverURL for httptest (87.4% coverage) — `db40d35`
- [x] Sandbox.ExecCommand interface + ProcessExecutor integration — `db40d35`
- [x] BuildAction with dep install pre-step (go mod download / npm install) — `db40d35`
- [x] Dogfooding smoke tests (--dir mode, project detection, rule engine) — `db40d35`

### Deferred (by design)

- [ ] Browser executor (click, input) — future phase
- [ ] Non-Go code analysis (Python, Node)
- [ ] Evidence binary encoding (base64)

### Known Limitations

- MCP endpoints default to nil; requires runtime configuration
