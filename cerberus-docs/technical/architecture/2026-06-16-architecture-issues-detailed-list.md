# Cerberus 架构问题详细清单

**生成时间**: 2026-06-16  
**总问题数**: 190个  
**架构健康度**: 0/100

---

## 📋 目录

- [高优先级问题 (P0)](#高优先级问题-p0---核心业务逻辑)
- [中优先级问题 (P1)](#中优先级问题-p1---支撑系统)
- [低优先级问题 (P2)](#低优先级问题-p2---测试和工具)
- [信息级问题 (Info)](#信息级问题-info---代码质量改进)
- [过早抽象问题](#过早抽象问题-18个接口)
- [SOLID原则违反](#solid原则违反-54个srp违反))

---

## 🔥 高优先级问题 (P0) - 核心业务逻辑

### 1. 会话生命周期管理 - Critical
**文件**: `internal/session/lifecycle.go`

**问题清单**:
- [x] #87: 文件过长 (401行，阈值150)
- [x] #88: `NewSession` 函数参数过多 (9个参数，阈值5)
- [x] #89: `Run` 函数圈复杂度23 (阈值10)
- [x] #90: `Run` 函数嵌套深度5 (阈值4)
- [x] #91: `Resume` 函数圈复杂度17 (阈值10)

**影响**: 会话管理是Cerberus的核心功能，影响整个系统稳定性

**修复建议**:
```go
// 1. 创建配置结构体
type SessionConfig struct {
    ID          string
    Mode        session.Mode
    Goal        string
    Store       store.Store
    Logger      *zap.Logger
    Head        head.Head
    Config      *config.Config
    Credentials *config.Credentials
}

func NewSession(cfg SessionConfig) (*Session, error) {
    // 简化参数传递
}

// 2. 拆分Run函数
func (s *Session) Run(ctx context.Context) error {
    if err := s.initialize(ctx); err != nil {
        return err
    }
    if err := s.executeHead(ctx); err != nil {
        return err
    }
    return s.finalize(ctx)
}

// 3. 提取子函数减少嵌套
func (s *Session) initialize(ctx context.Context) error {
    // 初始化逻辑
}

func (s *Session) executeHead(ctx context.Context) error {
    // 执行head逻辑
}

func (s *Session) finalize(ctx context.Context) error {
    // 清理逻辑
}
```

**预计工作量**: 4-6小时  
**风险**: 高 - 需要大量重构

---

### 2. Agent执行器 - Critical
**文件**: `internal/head/agent/executor.go`

**问题清单**:
- [x] #47: 文件过长 (438行，阈值150)
- [x] #48: `NewReActLoopWithGate` 函数参数过多 (7个参数，阈值5)
- [x] #49: `NewReActLoop` 函数参数过多 (6个参数，阈值5)
- [x] #50: `executeStep` 函数圈复杂度30 (阈值10)
- [x] #51: `executeStep` 函数嵌套深度5 (阈值4)
- [x] #162: 文件有4个职责 (testing, rendering, parsing, logging)

**影响**: Agent执行的核心逻辑，影响所有任务执行

**修复建议**:
```go
// 1. 提取执行策略接口
type ExecutionStepHandler interface {
    Handle(ctx context.Context, step *Step) error
}

// 2. 使用配置对象
type ExecutorConfig struct {
    MaxIterations int
    Gate         escalation.Gate
    Rules        []Rule
    Logger       *zap.Logger
}

func NewReActLoopWithGate(cfg ExecutorConfig) *Executor {
    return &Executor{
        config: cfg,
    }
}

// 3. 拆分executeStep为策略模式
func (e *Executor) executeStep(ctx context.Context, step *Step) error {
    handler := e.getHandlerForStep(step.Type)
    return handler.Handle(ctx, step)
}

// 4. 分离渲染和测试职责到独立文件
// - executor_render.go
// - executor_test.go
// - executor_parse.go
```

**预计工作量**: 6-8小时  
**风险**: 高 - 核心执行逻辑重构

---

### 3. 项目验证 - Critical
**文件**: `internal/project/validate.go`

**问题清单**:
- [x] #75: `Validate` 函数圈复杂度26 (阈值10)
- [x] #170: 文件有2个职责 (logging, validation)

**影响**: 项目配置验证失败会导致整个系统无法启动

**修复建议**:
```go
// 使用验证规则模式
type ValidationRule interface {
    Validate(cfg *Config) error
    Name() string
}

type NameRule struct{}
func (r *NameRule) Validate(cfg *Config) error {
    if cfg.Name == "" {
        return fmt.Errorf("name is required")
    }
    return nil
}
func (r *NameRule) Name() string { return "name" }

type ServicesRule struct{}
func (r *ServicesRule) Validate(cfg *Config) error {
    // 验证服务配置
    return nil
}
func (r *ServicesRule) Name() string { return "services" }

func Validate(cfg *Config) error {
    rules := []ValidationRule{
        &NameRule{},
        &ServicesRule{},
        &DatabasesRule{},
    }
    
    for _, rule := range rules {
        if err := rule.Validate(cfg); err != nil {
            return fmt.Errorf("%s: %w", rule.Name(), err)
        }
    }
    return nil
}
```

**预计工作量**: 2-3小时  
**风险**: 中

---

### 4. 策略验证 - Critical
**文件**: `internal/policy/default.go`

**问题清单**:
- [x] #74: `Validate` 函数圈复杂度24 (阈值10)

**影响**: 安全策略验证

**修复建议**: 采用与项目验证相同的规则模式

**预计工作量**: 2-3小时  
**风险**: 中

---

### 5. 类型系统解析 - Critical
**文件**: `internal/types/actions.go`

**问题清单**:
- [x] #97: 文件过长 (593行，阈值150)
- [x] #98: `derefAction` 函数圈复杂度22 (阈值10)
- [x] #180: 文件有5个职责 (validation, calculation, parsing, persistence, network)

**影响**: 类型系统是整个架构的基础

**修复建议**:
```go
// 拆分为多个文件：
// - actions_base.go - 基础类型定义
// - actions_validation.go - 验证逻辑
// - actions_parsing.go - 解析逻辑
// - actions_persistence.go - 持久化逻辑
// - actions_network.go - 网络传输逻辑
```

**预计工作量**: 4-6小时  
**风险**: 中

---

## 🟡 中优先级问题 (P1) - 支撑系统

### 6. LLM客户端重构
**影响文件**: 
- `internal/llm/claude.go` (#64-66)
- `internal/llm/gemini.go` (#67-69)
- `internal/llm/openai.go` (#70-71)
- `internal/llm/client.go` (#119, #167)

**共同问题**:
- 所有文件过长 (250-280行)
- `Complete` 和 `Stream` 函数圈复杂度过高 (12-16)
- Client接口被报告为过早抽象 (#119)
- 文件混合了network和configuration职责 (#167)

**修复建议**:
```go
// 1. 提取公共逻辑到base client
type BaseClient struct {
    client ClientConfig
    logger *zap.Logger
}

// 2. 使用中间件模式处理重试逻辑
type RetryMiddleware struct {
    maxRetries int
    backoff    func(attempt int) time.Duration
}

func (m *RetryMiddleware) Execute(ctx context.Context, req *Request) (*Response, error) {
    var lastErr error
    for i := 0; i < m.maxRetries; i++ {
        resp, err := m.client.Execute(ctx, req)
        if err == nil {
            return resp, nil
        }
        lastErr = err
        time.Sleep(m.backoff(i))
    }
    return nil, lastErr
}

// 3. 或者直接删除Client接口，使用具体类型
// 如果确实需要多实现，添加注释说明
// Client defines the interface for LLM providers
// Currently implemented by: ClaudeClient, GeminiClient, OpenAIClient
```

**预计工作量**: 8-10小时  
**风险**: 中 - 影响所有LLM调用

---

### 7. 存储层重构
**影响文件**: 
- `internal/store/regression.go` (#95, #176)
- `internal/store/verdict.go` (#96)
- `internal/store/procedural.go` (#94, #175)

**问题**:
- 文件过长 (165-299行)
- `CreateVerdict` 函数参数过多 (8个参数，阈值5)
- 多个文件混合了persistence和caching职责

**修复建议**:
```go
// 1. 使用构建者模式
type VerdictBuilder struct {
    verdict *Verdict
}

func NewVerdictBuilder() *VerdictBuilder {
    return &VerdictBuilder{verdict: &Verdict{}}
}

func (vb *VerdictBuilder) WithSession(id string) *VerdictBuilder {
    vb.verdict.SessionID = id
    return vb
}

func (vb *VerdictBuilder) WithTrace(trace int) *VerdictBuilder {
    vb.verdict.TraceID = trace
    return vb
}

func (vb *VerdictBuilder) WithStatus(status string) *VerdictBuilder {
    vb.verdict.Status = status
    return vb
}

func (vb *VerdictBuilder) Build() (*Verdict, error) {
    if err := vb.validate(); err != nil {
        return nil, err
    }
    return vb.verdict, nil
}

// 使用方式
verdict, err := NewVerdictBuilder().
    WithSession("session-1").
    WithTrace(123).
    WithStatus("pass").
    Build()

// 2. 分离缓存逻辑到独立文件
// - regression_cache.go
// - procedural_cache.go
```

**预计工作量**: 4-6小时  
**风险**: 中

---

### 8. 规则引擎优化
**文件**: `internal/head/agent/rules.go`

**问题**:
- [x] #57: `matchRules` 函数圈复杂度22 (阈值10)

**修复建议**:
```go
// 使用策略模式替代复杂的if-else
type RuleMatcher interface {
    Match(action Action) bool
    Apply(action *Action) error
}

type RegexMatcher struct {
    pattern string
}

func (rm *RegexMatcher) Match(action Action) bool {
    matched, _ := regexp.MatchString(rm.pattern, action.Name)
    return matched
}

func (rm *RegexMatcher) Apply(action *Action) error {
    // 应用规则逻辑
    return nil
}

type ExactMatcher struct {
    target string
}

func (em *ExactMatcher) Match(action Action) bool {
    return action.Name == em.target
}

func (em *ExactMatcher) Apply(action *Action) error {
    // 应用规则逻辑
    return nil
}
```

**预计工作量**: 3-4小时  
**风险**: 低

---

### 9. 报告生成器重构
**影响文件**: 
- `internal/report/markdown.go` (#79-80, #172)
- `internal/report/html.go` (#77)
- `internal/report/junit.go` (#78, #171)

**问题**:
- `RenderMarkdown` 圈复杂度30 (阈值10)
- 文件过长 (235-247行)
- 混合了rendering和logging/testing职责

**修复建议**:
```go
// 使用模板引擎
const markdownTemplate = `
# {{.Title}}

{{range .Sections}}
## {{.Name}}

{{.Content}}

{{end}}
`

func RenderMarkdown(data *ReportData) (string, error) {
    tmpl := template.Must(template.New("report").Parse(markdownTemplate))
    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, data); err != nil {
        return "", err
    }
    return buf.String(), nil
}

// 分离日志到独立文件
// - markdown_logger.go
// - junit_logger.go
```

**预计工作量**: 4-5小时  
**风险**: 低

---

### 10. Scout规划重构
**影响文件**: 
- `internal/head/scout/scout.go` (#61-62)
- `internal/head/scout/tot.go` (#63, #166)

**问题**:
- scout.go文件过长 (355行)
- `buildEpisodicContext` 圈复杂度11
- tot.go文件过长 (283行)
- 混合了configuration, calculation, rendering职责

**修复建议**:
```go
// 拆分scout.go为：
// - scout_base.go - 核心scout逻辑
// - scout_context.go - 上下文构建
// - scout_render.go - 渲染逻辑

// 拆分tot.go为：
// - tot_config.go - 配置处理
// - tot_calc.go - 计算逻辑
// - tot_render.go - 渲染逻辑
```

**预计工作量**: 4-6小时  
**风险**: 中

---

### 11. Dashboard重构
**文件**: `internal/dashboard/dashboard.go`

**问题**:
- [x] #39: 文件过长 (262行)
- [x] #40: `Update` 函数圈复杂度12
- [x] #41: `View` 函数圈复杂度18
- [x] #42: `View` 函数嵌套深度5

**修复建议**:
```go
// 提取视图逻辑
type ViewRenderer struct {
    dashboard *Dashboard
}

func (vr *ViewRenderer) RenderView(ctx context.Context) (string, error) {
    data, err := vr.dashboard.collectData(ctx)
    if err != nil {
        return "", err
    }
    return vr.renderTemplate(data)
}

// 提取更新逻辑
type UpdateHandler struct {
    dashboard *Dashboard
}

func (uh *UpdateHandler) HandleUpdate(ctx context.Context, input UpdateInput) error {
    if err := uh.validateInput(input); err != nil {
        return err
    }
    return uh.dashboard.applyUpdate(ctx, input)
}
```

**预计工作量**: 3-4小时  
**风险**: 低

---

### 12. Server重构
**文件**: `internal/server/server.go`

**问题**:
- [x] #85: 文件过长 (252行)
- [x] #86: `handleCreateSession` 圈复杂度13
- [x] #173: 混合了network, persistence, logging职责

**修复建议**:
```go
// 拆分为多个文件：
// - server_handlers.go - HTTP处理器
// - server_persistence.go - 持久化逻辑
// - server_logger.go - 日志记录

// 提取处理器
type SessionHandler struct {
    store  store.Store
    logger *zap.Logger
}

func (sh *SessionHandler) HandleCreate(ctx context.Context, req *CreateRequest) (*CreateResponse, error) {
    if err := sh.validateRequest(req); err != nil {
        return nil, err
    }
    session := sh.buildSession(req)
    if err := sh.store.Save(session); err != nil {
        return nil, err
    }
    return &CreateResponse{ID: session.ID}, nil
}
```

**预计工作量**: 3-4小时  
**风险**: 中

---

### 13. 架构分析器重构
**影响文件**: 
- `internal/architecture/abstraction.go` (#19-21)
- `internal/architecture/complexity.go` (#22)
- `internal/architecture/solid.go` (#25-27)
- `internal/architecture/scenarios.go` (#24)

**问题**:
- abstraction.go嵌套深度7和12
- complexity.go文件过长 (164行)
- solid.go文件过长 (158行)，`analyzeSRP`圈复杂度16和嵌套深度7
- scenarios.go的`analyzeScenarios`圈复杂度13

**修复建议**:
```go
// 1. 优化analyzeFileInterfaces减少嵌套
func (a *Analyzer) analyzeFileInterfaces(filePath string) ([]*InterfaceInfo, error) {
    file, err := a.parser.ParseFile(filePath)
    if err != nil {
        return nil, err
    }
    
    // 早期返回减少嵌套
    if file == nil {
        return nil, nil
    }
    
    interfaces := a.extractInterfaces(file)
    return a.analyzeImplementations(interfaces), nil
}

// 2. 拆分solid.go
// - solid_srp.go - SRP分析
// - solid_ocp.go - OCP分析
// - solid_lsp.go - LSP分析
// - solid_isp.go - ISP分析
// - solid_dip.go - DIP分析
```

**预计工作量**: 4-6小时  
**风险**: 中

---

### 14. MCP服务器重构
**影响文件**: 
- `internal/mcp/server.go` (#73)
- `internal/mcp/escalation.go` (#168)
- `internal/mcp/protocol.go` (#169)

**问题**:
- server.go文件过长 (335行)
- escalation.go混合validation和network职责
- protocol.go混合4个职责 (logging, parsing, persistence, network)

**修复建议**:
```go
// 拆分server.go为：
// - server_base.go - 服务器基础
// - server_handlers.go - 请求处理器
// - server_tools.go - 工具注册

// 拆分protocol.go为：
// - protocol_parse.go - 解析逻辑
// - protocol_persist.go - 持久化
// - protocol_network.go - 网络传输
// - protocol_logger.go - 日志记录
```

**预计工作量**: 4-5小时  
**风险**: 中

---

## 🟢 低优先级问题 (P2) - 测试和工具

### 15. 测试相关文件重构
**影响**: 35个问题，主要在 `internal/autotest/` 和测试代码中

**问题类型**: 文件过长、函数复杂度高、混合职责

**修复建议**: 
- 提取测试工具函数到单独的包
- 使用表驱动测试减少重复代码
- 分离测试的parsing, logging, testing职责

**预计工作量**: 10-15小时  
**风险**: 低 - 只影响测试代码

---

### 16. 文档处理脚本优化
**影响**: 24个问题，主要在 `learning/code/openclaw/scripts/docs-i18n/`

**问题类型**: 脚本文件过长、复杂度高、混合职责

**修复建议**:
- 这些是一次性迁移脚本，可以接受较低的质量标准
- 仅修复关键的可读性问题
- 不需要大规模重构

**预计工作量**: 2-4小时 (选择性修复)  
**风险**: 极低

---

### 17. AI模块重构
**影响文件**: 
- `internal/ai/business_understanding.go` (#9, #141)
- `internal/ai/comment_miner.go` (#10-11, #143)
- `internal/ai/coverage_optimizer.go` (#12)
- `internal/ai/driver.go` (#13-15, #144)
- `internal/ai/minimal_interaction.go` (#16, #146)
- `internal/ai/parser.go` (#17, #147)
- `internal/ai/pattern_recognizer.go` (#18, #148)

**问题**:
- 所有文件过长 (174-320行)
- 多个函数圈复杂度过高
- 混合了多种职责 (calculation, validation, parsing, logging, persistence, rendering)

**修复建议**:
```go
// 拆分business_understanding.go为：
// - understanding_calculator.go
// - understanding_validator.go
// - understanding_persister.go
// - understanding_renderer.go
// - understanding_parser.go

// 使用专门的logger而不是直接在业务逻辑中混合
type BusinessUnderstandingService struct {
    calculator *UnderstandingCalculator
    validator  *UnderstandingValidator
    persister  *UnderstandingPersister
    renderer   *UnderstandingRenderer
    parser     *UnderstandingParser
}
```

**预计工作量**: 12-16小时  
**风险**: 中

---

### 18. Agent相关模块重构
**影响文件**: 
- `internal/head/agent/browser.go` (#43)
- `internal/head/agent/code.go` (#44-46, #161)
- `internal/head/agent/mcp_exec.go` (#52)
- `internal/head/agent/multi.go` (#53)
- `internal/head/agent/parallel.go` (#54-55)
- `internal/head/agent/process.go` (#56)
- `internal/head/agent/file.go` (#163)
- `internal/head/agent/types.go` (#164)

**问题**:
- 多个文件过长 (160-399行)
- 多个函数圈复杂度11-30
- 混合了多种职责

**修复建议**:
```go
// 拆分code.go为：
// - code_parser.go
// - code_validator.go
// - code_logger.go

// 拆分parallel.go为：
// - parallel_executor.go
// - parallel_detector.go
```

**预计工作量**: 10-14小时  
**风险**: 中

---

### 19. Examiner重构
**影响文件**: 
- `internal/head/examiner/learner.go` (#58, #165)
- `internal/head/examiner/strategy_matcher.go` (#59)
- `internal/head/examiner/verdict_persist.go` (#60)

**问题**:
- `buildReflectionContext` 嵌套深度5
- `globMatchRecursive` 圈复杂度11
- `ClassifyFailureReason` 圈复杂度11
- 混合了persistence和caching职责

**修复建议**:
```go
// 提取缓存逻辑到独立文件
// - learner_cache.go

// 优化函数减少嵌套
func (el *Learner) buildReflectionContext(ctx context.Context) (*ReflectionContext, error) {
    // 早期返回
    if el.session == nil {
        return nil, fmt.Errorf("no active session")
    }
    
    // 提取子函数
    results, err := el.collectResults(ctx)
    if err != nil {
        return nil, err
    }
    
    patterns, err := el.identifyPatterns(results)
    if err != nil {
        return nil, err
    }
    
    return &ReflectionContext{
        Results:  results,
        Patterns: patterns,
    }, nil
}
```

**预计工作量**: 4-6小时  
**风险**: 低

---

## 🔵 信息级问题 (Info) - 代码质量改进

### 20. 嵌套深度优化
**数量**: 8个问题

**影响文件**:
- `cmd/cerberus/regression.go` (#7-8)
- `internal/architecture/abstraction.go` (#19, #21)
- `internal/architecture/dependencies.go` (#23)
- `internal/head/agent/code.go` (#45)
- `internal/dashboard/dashboard.go` (#42)
- `internal/head/examiner/learner.go` (#58)

**修复建议**:
```go
// 使用早期返回减少嵌套
func process(data []string) error {
    if len(data) == 0 {
        return nil
    }
    
    for _, item := range data {
        if item == "" {
            continue
        }
        // 处理逻辑
    }
    return nil
}

// 提取子函数
func analyzeDeep(data interface{}) (Result, error) {
    if err := validate(data); err != nil {
        return Result{}, err
    }
    
    parsed, err := parse(data)
    if err != nil {
        return Result{}, err
    }
    
    return compute(parsed), nil
}
```

**预计工作量**: 2-3小时  
**风险**: 极低

---

## 🚫 过早抽象问题 (18个接口)

### 完整列表

| # | 接口 | 文件 | 建议处理 |
|---|------|------|----------|
| 119 | Client | internal/llm/client.go | 保留 (3个实现) |
| 120 | TestGenerator | internal/autotest/provider.go | 删除或文档化 |
| 121 | Gate | internal/escalation/gate.go | 保留 (NoOpGate, MCPGate) |
| 122 | recoverer | internal/head/agent/executor.go | 删除或文档化 |
| 123 | TypedExecutor | internal/head/agent/multi.go | 删除或文档化 |
| 124 | ExecutorPlugin | internal/head/agent/plugin.go | 删除或文档化 |
| 125 | Sandbox | internal/sandbox/sandbox.go | 保留 (Linux, NoOp) |
| 126 | docsTranslator | learning/.../translator.go | 删除或文档化 |
| 127 | ProjectDetector | internal/autotest/detector.go | 删除或文档化 |
| 128 | Provider | internal/embed/provider.go | 保留 (TrigramProvider) |
| 129 | TypedAction | internal/types/actions.go | 删除或文档化 |
| 130 | Writer | internal/autotest/autotest.go | 删除或文档化 |
| 131 | CoverageProvider | internal/autotest/provider.go | 删除或文档化 |
| 132 | Detector | internal/detect/detect.go | 删除或文档化 |
| 133 | ActionPolicy | internal/policy/policy.go | 删除或文档化 |
| 134 | ExecutorResult | internal/types/result.go | 删除或文档化 |
| 135 | RequestGate | internal/autotest/autotest.go | 删除或文档化 |
| 136 | RulePlugin | internal/head/agent/plugin.go | 删除或文档化 |

**处理策略**:

```go
// 方案1: 如果接口只有一个实现，考虑移除接口
// 将接口改为具体类型

// 方案2: 如果接口是为未来扩展预留，添加注释说明
// Client defines the interface for LLM providers
// This abstraction allows for easy addition of new providers
// Currently implemented by: ClaudeClient, GeminiClient, OpenAIClient
type Client interface {
    Complete(ctx context.Context, prompt string) (string, error)
}

// 方案3: 如果接口确实不需要，直接删除
```

**预计工作量**: 4-6小时  
**风险**: 低 - 大部分是文档化工作

---

## 📋 SOLID原则违反 (54个SRP违反)

### 职责混合统计

| 职责组合 | 数量 | 典型文件 |
|---------|------|----------|
| validation + logging | 12 | internal/project/validate.go |
| parsing + testing | 8 | internal/ai/parser.go |
| persistence + caching | 6 | internal/store/ |
| configuration + calculation | 5 | internal/ai/ |
| rendering + logging | 5 | internal/report/ |
| testing + logging | 4 | internal/autotest/ |

### 典型修复模式

```go
// 分离职责前
func (s *Service) process() error {
    // 验证逻辑
    if err := s.validate(); err != nil {
        s.logger.Error("validation failed", zap.Error(err))
        return err
    }
    
    // 计算逻辑
    result := s.calculate()
    
    // 持久化逻辑
    if err := s.save(result); err != nil {
        return err
    }
    
    return nil
}

// 分离职责后
type Validator interface {
    Validate(data interface{}) error
}

type Calculator interface {
    Calculate(data interface{}) (interface{}, error)
}

type Persister interface {
    Save(data interface{}) error
}

// 主函数只负责协调
func (s *Service) process() error {
    if err := s.validator.Validate(s.data); err != nil {
        return err
    }
    
    result, err := s.calculator.Calculate(s.data)
    if err != nil {
        return err
    }
    
    return s.persister.Save(result)
}
```

**预计工作量**: 20-30小时 (全部54个违反)  
**风险**: 中 - 需要重构大量代码

---

## 🎯 推荐修复顺序

### 第一阶段 (1-2周) - Critical Fixes
1. ✅ 会话生命周期重构 (#87-91) - 6小时
2. ✅ Agent执行器重构 (#47-51, #162) - 8小时
3. ✅ 项目验证优化 (#75, #170) - 3小时
4. ✅ 策略验证优化 (#74) - 3小时
5. ✅ 类型系统解析 (#97-98, #180) - 6小时

**总计**: ~26小时  
**影响**: 修复5个最关键的复杂度问题

---

### 第二阶段 (2-3周) - Support Systems
1. ✅ LLM客户端重构 (#64-71, #119, #167) - 10小时
2. ✅ 存储层重构 (#94-96, #175-176) - 6小时
3. ✅ 规则引擎优化 (#57) - 4小时
4. ✅ 报告生成器重构 (#77-80, #171-172) - 5小时
5. ✅ Scout规划重构 (#61-63, #166) - 6小时
6. ✅ Dashboard重构 (#39-42) - 4小时
7. ✅ Server重构 (#85-86, #173) - 4小时
8. ✅ 架构分析器重构 (#19-27) - 6小时

**总计**: ~45小时  
**影响**: 改善支撑系统的可维护性

---

### 第三阶段 (3-4周) - Quality Improvements
1. ✅ SOLID原则违反修复 (#137-190) - 30小时
2. ✅ 测试代码质量改进 - 15小时
3. ✅ 嵌套深度优化 (#7-8, #19, #21, #23, #42, #45, #58) - 3小时
4. ✅ 过早抽象清理 (#119-136) - 6小时
5. ✅ AI模块重构 (#9-18, #141-148) - 16小时
6. ✅ Agent相关模块重构 (#43-56, #161-164) - 14小时

**总计**: ~84小时  
**影响**: 提升整体代码质量

---

## 📈 预期改进

### 修复前
- 架构健康度: 0/100
- 总问题数: 190
- 平均文件行数: 119
- 最大文件行数: 774

### 修复后 (预期)
- 架构健康度: 75-85/100
- 总问题数: <30
- 平均文件行数: <100
- 最大文件行数: <300

### 具体改进
- ✅ **可维护性**: 拆分大文件，降低圈复杂度
- ✅ **可测试性**: 减少函数参数，提取纯函数
- ✅ **可读性**: 减少嵌套深度，改进命名
- ✅ **可扩展性**: 移除过早抽象，保留必要抽象

---

## 🛠️ 修复工具和流程

### 推荐工具
1. **golangci-lint**: 自动检测复杂度问题
2. **goimports**: 自动格式化和导入管理
3. **gorename**: 安全重构函数和变量
4. **gocov**: 测试覆盖率验证

### 修复流程
```bash
# 1. 选择要修复的文件
# 2. 运行回归测试确保安全
cerberus regression

# 3. 进行重构
# 4. 运行测试
go test ./...

# 5. 再次运行架构分析
cerberus architecture

# 6. 验证问题已修复
```

### 验证标准
- ✅ 所有测试通过
- ✅ 回归测试通过
- ✅ 架构健康度提升
- ✅ 新代码不引入新问题

---

## 📝 问题清单索引

### 按文件索引

| 文件 | 问题编号 | 类型 | 优先级 |
|------|---------|------|--------|
| cmd/cerberus/main.go | 1-4, 139 | 复杂度, SRP | 高 |
| cmd/cerberus/regression.go | 5-8, 140 | 复杂度, SRP | 高 |
| internal/session/lifecycle.go | 87-91 | 复杂度 | 高 |
| internal/head/agent/executor.go | 47-51, 162 | 复杂度, SRP | 高 |
| internal/project/validate.go | 75, 170 | 复杂度, SRP | 高 |
| internal/policy/default.go | 74 | 复杂度 | 高 |
| internal/types/actions.go | 97-98, 180 | 复杂度, SRP | 高 |
| internal/llm/*.go | 64-72, 119, 167 | 复杂度, 抽象 | 中 |
| internal/store/*.go | 94-96, 175-176 | 复杂度, SRP | 中 |
| internal/report/*.go | 77-80, 171-172 | 复杂度, SRP | 中 |
| internal/ai/*.go | 9-18, 141-148 | 复杂度, SRP | 中 |

---

**文档版本**: v1.0  
**最后更新**: 2026-06-16  
**维护者**: Cerberus架构团队
