# Cerberus 质量检查框架设计文档

**文档版本:** 2.1
**创建日期:** 2026-06-16
**最后修订:** 2026-06-16
**作者:** Claude (Cerberus Project)
**状态:** 待审查

**版本变更:**
- v2.1: 最少交互策略 - AI自主推断优先
- v2.0: 转向AI主导架构
- v1.0: 初始版本（工具为主）

---

## 文档概述

本文档描述了Cerberus质量检查框架的完整设计，采用**AI主导、工具辅助**的架构，旨在通过AI深度理解业务逻辑和自动生成高质量测试，显著提升代码质量和测试覆盖率。

### 设计理念转变

```yaml
从: "工具检测为主，AI辅助"
到: "AI主导理解，最少交互，工具辅助验证"
```

### 核心目标

1. **AI自主业务理解**（零交互优先）
   - 从代码结构、深度注释挖掘、模式识别推断业务
   - 强化注释语义提取，最大限度减少人工输入
   - 构建持久化的业务模型，展示AI推断结果

2. **AI主导测试生成**
   - 基于业务模型生成完整场景覆盖
   - 自动识别边界场景和组合测试
   - 迭代优化直到覆盖率达标

3. **最少交互策略**
   - AI优先自主推断，置信度>60%不询问
   - 只在必要时询问1-2个关键问题（兜底）
   - 提供默认推断值，用户可直接确认或修正

4. **工具辅助验证**
   - 验证AI生成的发现
   - 执行AI生成的测试
   - 提供客观指标（覆盖率、复杂度）

### 关键架构决策

| 决策点 | 选择方案 | 理由 |
|--------|----------|------|
| **核心理念** | AI自主推断、最少交互 | 最大化自动化，最小化用户输入 |
| **业务理解** | 强化注释挖掘+模式识别 | 从代码中提取更多信息，减少询问 |
| **交互策略** | 置信度驱动 | 置信度高不问，低时只问关键问题 |
| **测试生成** | AI场景生成 | AI能识别业务规则组合和边界场景 |
| **覆盖优化** | AI迭代优化 | 通过执行反馈持续改进覆盖率 |
| **工具角色** | 验证和执行 | 工具提供客观指标，验证AI的发现 |

---

## 1. AI主导架构概览

### 1.1 系统分层

```
┌─────────────────────────────────────────────────────────────┐
│              AI Autonomous Understanding Layer              │
│  - Deep Code Analysis + Aggressive Comment Mining          │
│  - Pattern Recognition + Business Model Inference           │
│  - Minimal Interaction (1-2 critical questions if needed)    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              AI Test Generation Engine                     │
│  - Scenario Generation + Edge Case Detection              │
│  - Business Rule Combinations + Test Code Generation       │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              AI Coverage Optimizer                         │
│  - Gap Analysis + Iterative Test Generation               │
│  - Quality Assessment + Continuous Improvement             │
└─────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┴───────────────┐
              ▼                               ▼
┌─────────────────────────┐     ┌─────────────────────────┐
│   Tool Validation       │     │   Test Execution        │
│  - Verify AI findings   │     │  - Run generated tests  │
│  - Objective metrics    │     │  - Collect results      │
└─────────────────────────┘     └─────────────────────────┘
```

### 1.2 核心组件关系

```go
// 主要组件及其职责
AIBusinessUnderstanding  → AI自主理解业务逻辑
  ├─ CodeAnalyzer        → 深度分析代码结构
  ├─ CommentMiner        → 强化挖掘注释语义
  ├─ PatternRecognizer   → 识别业务模式
  ├─ SemanticAnalyzer    → 语义分析和推断
  └─ MinimalInteraction   → 最少交互（必要时）

AITestGenerator         → AI生成测试场景
  ├─ ScenarioGenerator   → 生成正常/边界/错误场景
  ├─ CombinationEngine    → 识别业务规则组合
  └─ CodeGenerator       → 生成测试代码

AICoverageOptimizer      → AI优化覆盖率
  ├─ GapAnalyzer         → 分析覆盖缺口
  ├─ IterativeGenerator  → 迭代生成补充测试
  └─ QualityAssessor     → 评估测试质量

ToolValidation          → 工具验证
  ├─ CompileChecker      → 编译时错误验证
  ├─ StaticAnalyzer      → 静态分析验证
  └─ CoverageReporter    → 覆盖率报告
```

---

## 2. AI业务理解引擎（最少交互）

### 2.1 AI自主推断架构

```go
// internal/ai/business_understanding.go
package ai

type BusinessUnderstandingEngine struct {
    // AI自主分析工具
    codeAnalyzer      *CodeAnalyzer
    commentMiner      *CommentMiner      // 强化注释挖掘
    patternRecognizer  *PatternRecognizer  // 业务模式识别
    semanticAnalyzer   *SemanticAnalyzer   // 语义分析
    llmClient         llm.Client
    memory            *BusinessMemory
}

func (e *BusinessUnderstandingEngine) UnderstandProject(projectPath string) (*BusinessModel, error) {
    // 阶段1: 深度代码分析（不交互）
    codeInsights := e.analyzeCodeDeeply(projectPath)
    
    // 阶段2: 强化注释挖掘（不交互）
    semanticInsights := e.mineCommentsAggressively(projectPath)
    
    // 阶段3: 业务模式识别（不交互）
    patterns := e.recognizeBusinessPatterns(codeInsights, semanticInsights)
    
    // 阶段4: AI综合推断（不交互）
    businessModel := e.inferBusinessModel(codeInsights, semanticInsights, patterns)
    
    // 阶段5: 只在必要时询问（兜底）
    if e.isConfidenceLow(businessModel) {
        questions := e.generateCriticalQuestionsOnly(businessModel)
        if len(questions) > 0 {
            // 只询问最关键的1-2个问题
            answers := e.askMinimalQuestions(questions)
            businessModel = e.refineWithAnswers(businessModel, answers)
        }
    }
    
    // 阶段6: 保存并展示推断结果
    e.saveAndDisplay(businessModel)
    
    return businessModel, nil
}
```

### 2.2 业务模型结构（增强）

```go
// internal/ai/business_model.go
type BusinessModel struct {
    // 元信息
    ID          string
    ProjectPath string
    GeneratedAt time.Time
    Version     int
    
    // AI推断的置信度
    Confidence  float64  // 0.0-1.0，整体置信度
    InferenceSource string  // "ai_autonomous" | "ai_assisted" | "manual"
    
    // 业务领域识别
    Domain      string  // "e-commerce", "finance", "social-network"
    DomainConfidence float64  // 领域识别的置信度
    
    // 核心业务概念
    Concepts []BusinessConcept
    type BusinessConcept struct {
        Name        string
        Type        string  // "entity", "value_object", "service"
        Description string
        RelatedTo   []string
        Inferred    bool    // 是否为AI推断
        Confidence  float64 // 推断置信度
    }
    
    // 业务规则
    Rules []BusinessRule
    type BusinessRule struct {
        ID          string
        Name        string
        Description string
        Condition   string
        Effect      string
        Priority    string  // "critical", "high", "medium", "low"
        Examples    []string
        Source      string  // "comment" | "code_pattern" | "inferred"
        Confidence  float64 // 规则推断置信度
    }
    
    // 业务流程
    Workflows []Workflow
    type Workflow struct {
        Name        string
        Steps       []WorkflowStep
        EntryPoints []string
        ExitPoints  []string
        Inferred    bool
    }
    
    // 约束和不变式
    Invariants []Invariant
    type Invariant struct {
        Description string
        Expression  string
        Scope       string
        Source      string  // "execution_observed" | "inferred"
    }
    
    // 边界场景（AI推断）
    EdgeCases []EdgeCase
    type EdgeCase struct {
        Name        string
        Description string
        Trigger     string
        Expected    string
        Rationale   string
        Confidence  float64
    }
    
    // 错误场景
    ErrorScenarios []ErrorScenario
    type ErrorScenario struct {
        Name        string
        Trigger     string
        Handling    string
        Recovery    string
    }
}
```

### 2.3 深度代码分析

```go
// internal/ai/code_analyzer.go
type CodeAnalyzer struct {
    parser *CodeParser
    llmClient llm.Client
}

func (a *CodeAnalyzer) AnalyzeDeeply(projectPath string) *CodeInsights {
    insights := &CodeInsights{}
    
    // 1. 深度结构分析
    structure := a.parser.ParseStructure(projectPath)
    insights.Modules = a.identifyModules(structure)
    insights.Layers = a.identifyLayers(structure)
    insights.Responsibilities = a.analyzeResponsibilities(structure)
    
    // 2. 依赖关系深度分析
    dependencies := a.analyzeDependencies(insights.Modules)
    insights.DependencyGraph = a.buildDependencyGraph(dependencies)
    insights.Coupling = a.analyzeCoupling(dependencies)
    
    // 3. API接口语义分析
    apis := a.identifyAPIs(structure)
    insights.APIContracts = a.analyzeAPIContracts(apis)
    
    // 4. 数据流追踪
    dataflows := a.traceDataFlows(structure)
    insights.DataTransformations = a.identifyTransformations(dataflows)
    
    // 5. 状态机识别
    insights.StateMachines = a.identifyStateMachines(structure)
    
    return insights
}
```

### 2.4 强化注释挖掘

```go
// internal/ai/comment_miner.go
type CommentMiner struct {
    llmClient llm.Client
}

func (m *CommentMiner) MineAggressively(projectPath string) *SemanticInsights {
    insights := &SemanticInsights{}
    
    // 1. 挖掘所有类型的注释（不仅仅是函数注释）
    sources := []CommentSource{
        SingleLineComments,
        MultiLineComments,
        DocComments,
        InlineComments,
        PackageComments,
        FileHeaderComments,
        TODOComments,
        FIXMEComments,
        NOTEComments,
        HACKComments,  // HACK注释通常包含重要业务逻辑
        WARNINGComments,  // WARNING注释包含边界条件
    }
    
    for _, source := range sources {
        comments := extractCommentsFromSource(projectPath, source)
        for _, comment := range comments {
            // 2. 使用LLM深度理解每个注释
            semantic := m.understandCommentSemantics(comment)
            insights.Add(semantic)
        }
    }
    
    // 3. 关联相关注释
    insights.RelatedComments = m.findRelatedComments(insights.AllComments)
    
    // 4. 构建业务术语表
    insights.BusinessGlossary = m.buildGlossary(insights)
    
    // 5. 识别业务规则（从注释中）
    insights.BusinessRules = m.extractRulesFromComments(insights)
    
    // 6. 识别业务约束
    insights.Constraints = m.extractConstraintsFromComments(insights)
    
    return insights
}

func (m *CommentMiner) understandCommentSemantics(comment Comment) *CommentSemantics {
    prompt := fmt.Sprintf(
`深度分析以下代码注释的业务语义：

注释内容:
"""
%s
"""

上下文信息:
- 文件: %s
- 函数: %s
- 行号: %d
- 注释类型: %s
- 所在代码块: %s

请提取：
1. 业务概念（这个注释描述了什么业务概念？）
2. 业务规则（是否包含业务规则？具体是什么规则？）
3. 业务约束（是否包含约束条件？具体是什么约束？）
4. 边界场景（是否提到边界情况？）
5. 错误处理（是否提到错误处理逻辑？）
6. 业务理由（为什么这样设计？）
7. 业务术语（是否有特殊的业务术语？）

对于每个提取的信息，说明：
- 信息类型
- 具体内容
- 置信度（high/medium/low）
- 推断依据

以JSON格式返回。`, 
        comment.Text, 
        comment.File, 
        comment.Function, 
        comment.Line, 
        comment.Type,
        comment.CodeBlock,
    )
    
    response := m.llmClient.Call(prompt)
    return parseCommentSemantics(response)
}
```

### 2.5 业务模式识别

```go
// internal/ai/pattern_recognizer.go
type PatternRecognizer struct {
    llmClient llm.Client
    patternDB *PatternDatabase
}

func (r *PatternRecognizer) RecognizeBusinessPatterns(codeInsights *CodeInsights, semanticInsights *SemanticInsights) *BusinessPatterns {
    patterns := &BusinessPatterns{}
    
    // 1. 识别业务领域模式
    patterns.Domain = r.identifyDomainPattern(codeInsights)
    
    // 2. 识别工作流模式
    patterns.Workflows = r.identifyWorkflowPatterns(codeInsights, semanticInsights)
    
    // 3. 识别状态机模式
    patterns.StateMachines = r.identifyStateMachines(codeInsights)
    
    // 4. 识别业务规则模式
    patterns.Rules = r.identifyRulePatterns(semanticInsights)
    
    // 5. 识别边界模式
    patterns.EdgeCases = r.identifyEdgeCasePatterns(codeInsights, semanticInsights)
    
    // 6. 识别错误处理模式
    patterns.ErrorHandling = r.identifyErrorHandlingPatterns(semanticInsights)
    
    return patterns
}

func (r *PatternRecognizer) identifyDomainPattern(codeInsights *CodeInsights) DomainPattern {
    prompt := fmt.Sprintf(
`基于以下代码结构，识别业务领域：

代码结构:
- 模块: %s
- 主要实体: %s
- 核心服务: %s
- API端点: %s
- 数据模型: %s

请识别：
1. 业务领域（如：e-commerce, finance, social-network, crm, etc.）
2. 置信度（high/medium/low）
3. 识别依据（为什么是这个领域？）

只返回JSON格式。`, 
        formatModules(codeInsights.Modules),
        formatEntities(codeInsights.Entities),
        formatServices(codeInsights.Services),
        formatAPIs(codeInsights.APIs),
        formatDataModels(codeInsights.DataModels),
    )
    
    response := r.llmClient.Call(prompt)
    return parseDomainPattern(response)
}
```

### 2.6 最少交互策略

```go
// internal/ai/minimal_interaction.go
type MinimalInteraction struct {
    llmClient llm.Client
    config    *InteractionConfig
}

type InteractionConfig struct {
    AutoInferThreshold float64  // 置信度>此值不询问，默认0.6
    MaxQuestions        int     // 最多问题数量，默认2
    UseDefaults         bool    // 使用AI推断作为默认值
    AllowSkip           bool    // 允许跳过问题
}

func (m *MinimalInteraction) isConfidenceLow(model *BusinessModel) bool {
    // 只有以下情况才需要询问：
    return model.Confidence < m.config.AutoInferThreshold ||  // 整体置信度<60%
           len(model.Rules) == 0 ||                          // 没有识别到业务规则
           model.Domain == "unknown" ||                      // 无法识别业务领域
           model.DomainConfidence < 0.5                      // 领域识别置信度<50%
}

func (m *MinimalInteraction) generateCriticalQuestionsOnly(model *BusinessModel) []Question {
    questions := []Question{}
    
    // 只询问最关键的1-2个问题
    if model.Domain == "unknown" || model.DomainConfidence < 0.5 {
        questions = append(questions, Question{
            ID:    "domain_hint",
            Text:  "这个系统的主要业务领域是什么？",
            Type:  "multiple_choice",
            Options: []string{"e-commerce", "finance", "social-network", "crm", "other"},
            Hint:  "AI无法自动识别领域，请帮助",
        })
    }
    
    if len(model.Rules) == 0 || m.countLowConfidenceRules(model) > 3 {
        questions = append(questions, Question{
            ID:    "rules_hint",
            Text:  "系统中最重要的1-2个业务规则是什么？",
            Type:  "open_ended",
            Hint:  "AI未能从代码中识别到足够的业务规则",
            Examples: []string{
                "订单支付后不能取消",
                "VIP用户享受8折优惠",
                "库存不足时取消订单",
            },
        })
    }
    
    // 最多2个问题
    if len(questions) > m.config.MaxQuestions {
        questions = questions[:m.config.MaxQuestions]
    }
    
    return questions
}

func (m *MinimalInteraction) askMinimalQuestions(questions []Question) map[string]string {
    answers := make(map[string]string)
    
    for _, question := range questions {
        fmt.Printf("\n❓ %s\n", question.Text)
        
        // 显示AI推断（如果有）
        if aiInference := m.getAIInferenceForQuestion(question); aiInference != "" {
            fmt.Printf("   🤖 AI推断: %s\n", aiInference)
        }
        
        if question.Hint != "" {
            fmt.Printf("   💡 提示: %s\n", question.Hint)
        }
        
        if len(question.Examples) > 0 {
            fmt.Printf("   📝 示例: %s\n", strings.Join(question.Examples, "|"))
        }
        
        // 提供默认选项
        fmt.Printf("   [默认] %s", m.getDefaultAnswer(question))
        
        answer := m.promptForAnswer(question)
        if answer == "" {
            // 用户跳过，使用默认值
            answer = m.getDefaultAnswer(question)
        }
        
        answers[question.ID] = answer
    }
    
    return answers
}
```

### 2.7 AI综合推断

```go
func (e *BusinessUnderstandingEngine) inferBusinessModel(codeInsights *CodeInsights, semanticInsights *SemanticInsights, patterns *BusinessPatterns) *BusinessModel {
    prompt := fmt.Sprintf(
`基于以下信息，推断完整的业务模型：

代码洞察:
%s

语义洞察:
%s

业务模式:
%s

请构建完整的业务模型，包括：
1. 业务领域（尽可能准确）
2. 核心业务概念
3. 业务规则（尽可能从注释和代码中推断）
4. 工作流（从调用模式中推断）
5. 边界场景（从数据流和约束中推断）

对于每项信息，提供：
- 具体内容
- 置信度（high/medium/low）
- 推断依据（从什么信息推断出来的）
- 不确定性说明（如果不确定）

不要编造信息。如果不确定，标记为low confidence并说明不确定性。

以JSON格式返回。`, 
        formatCodeInsights(codeInsights),
        formatSemanticInsights(semanticInsights),
        formatPatterns(patterns),
    )
    
    response := e.llmClient.Call(prompt)
    businessModel := parseBusinessModel(response)
    
    // 添加元数据
    businessModel.Confidence = e.calculateOverallConfidence(businessModel)
    businessModel.InferenceSource = "ai_autonomous"
    businessModel.GeneratedAt = time.Now()
    
    return businessModel
}
```

---

## 3. AI测试生成引擎

### 3.1 场景生成架构

```go
// internal/ai/test_generator.go
type AITestGenerator struct {
    businessModel *BusinessModel
    llmClient     llm.Client
    codeAnalyzer  *CodeAnalyzer
}

func (g *AITestGenerator) GenerateTestSuite(targetFunction string) (*TestSuite, error) {
    // 1. 分析目标函数
    funcInfo := g.codeAnalyzer.AnalyzeFunction(targetFunction)
    
    // 2. 获取相关业务规则
    relevantRules := g.businessModel.FindRelevantRules(targetFunction)
    
    // 3. 生成测试场景
    scenarios := g.generateScenarios(funcInfo, relevantRules)
    
    // 4. 为每个场景生成测试代码
    tests := make([]*TestCase, len(scenarios))
    for i, scenario := range scenarios {
        testCode, err := g.generateTestCode(scenario, funcInfo)
        if err != nil {
            return nil, fmt.Errorf("failed to generate test for %s: %w", scenario.Name, err)
        }
        tests[i] = &TestCase{
            Scenario:  scenario,
            Code:      testCode,
            Generated: time.Now(),
        }
    }
    
    return &TestSuite{
        Function:    targetFunction,
        FunctionInfo: funcInfo,
        Scenarios:   scenarios,
        Tests:       tests,
        GeneratedAt: time.Now(),
    }, nil
}

func (g *AITestGenerator) generateScenarios(funcInfo FuncInfo, rules []BusinessRule) []Scenario {
    scenarios := []Scenario{}
    
    // 1. 正常场景
    normalScenarios := g.generateNormalScenarios(funcInfo, rules)
    scenarios = append(scenarios, normalScenarios...)
    
    // 2. 边界场景
    edgeScenarios := g.generateEdgeScenarios(funcInfo, rules)
    scenarios = append(scenarios, edgeScenarios...)
    
    // 3. 错误场景
    errorScenarios := g.generateErrorScenarios(funcInfo, rules)
    scenarios = append(scenarios, errorScenarios...)
    
    // 4. 业务规则组合场景
    combinationScenarios := g.generateCombinations(funcInfo, rules)
    scenarios = append(scenarios, combinationScenarios...)
    
    return scenarios
}
```

### 3.2 正常场景生成

```go
func (g *AITestGenerator) generateNormalScenarios(funcInfo FuncInfo, rules []BusinessRule) []Scenario {
    prompt := fmt.Sprintf(`
基于以下信息，生成正常业务场景：

函数信息:
- 名称: %s
- 参数: %s
- 返回值: %s
- 业务逻辑: %s

相关业务规则:
%s

请生成3-5个正常业务场景，每个场景包含：
- 名称
- 描述
- 输入参数
- 预期输出
- 业务理由

以JSON格式返回。
`, funcInfo.Name, funcInfo.Parameters, funcInfo.ReturnType, funcInfo.Logic, formatRules(rules))
    
    response := g.llmClient.Call(prompt)
    return parseScenarios(response)
}
```

### 3.3 边界场景生成

```go
func (g *AITestGenerator) generateEdgeScenarios(funcInfo FuncInfo, rules []BusinessRule) []Scenario {
    prompt := fmt.Sprintf(`
基于以下信息，生成边界场景：

函数信息:
%s

业务规则:
%s

请识别并生成边界场景测试，包括：
- 参数边界值（最大、最小、零）
- 状态边界（如库存刚好为0）
- 时间边界（如过期时刻）
- 数值精度边界
- 组合边界（多个条件同时触发）

对于每个场景，说明为什么这是边界情况。

以JSON格式返回。
`, formatFuncInfo(funcInfo), formatRules(rules))
    
    response := g.llmClient.Call(prompt)
    return parseScenarios(response)
}
```

### 3.4 业务规则组合场景

```go
func (g *AITestGenerator) generateCombinations(funcInfo FuncInfo, rules []BusinessRule) []Scenario {
    prompt := fmt.Sprintf(`
基于以下业务规则，生成所有有意义的组合测试场景：

函数: %s
业务规则:
%s

请考虑：
1. 规则的交集（多个规则同时满足）
2. 规则的冲突（规则之间的矛盾）
3. 优先级关系（规则之间的优先级）
4. 边界值组合（多个边界条件同时触发）

对于每个组合场景：
- 说明组合了哪些规则
- 说明预期的行为（优先级、冲突解决）
- 说明业务理由

以JSON格式返回。
`, funcInfo.Name, formatRules(rules))
    
    response := g.llmClient.Call(prompt)
    return parseScenarios(response)
}
```

### 3.5 测试代码生成

```go
func (g *AITestGenerator) generateTestCode(scenario Scenario, funcInfo FuncInfo) (string, error) {
    prompt := fmt.Sprintf(`
为以下测试场景生成测试代码：

场景信息:
- 名称: %s
- 描述: %s
- 输入: %s
- 预期输出: %s

函数信息:
- 语言: %s
- 签名: %s
- 包路径: %s

要求：
1. 使用标准的测试框架（Go: testing, Node.js: jest, Python: pytest）
2. 包含清晰的setup和teardown
3. 包含有意义的断言
4. 添加必要的注释说明业务逻辑
5. 处理可能的错误情况

只返回测试代码，不要其他说明。
`, 
    scenario.Name, 
    scenario.Description, 
    formatInput(scenario.Input),
    formatOutput(scenario.Expected),
    funcInfo.Language,
    funcInfo.Signature,
    funcInfo.PackagePath,
)
    
    response := g.llmClient.Call(prompt)
    return extractCodeFromResponse(response)
}
```

---

## 4. AI覆盖优化引擎

### 4.1 覆盖缺口分析

```go
// internal/ai/coverage_optimizer.go
type CoverageOptimizer struct {
    llmClient        llm.Client
    testRunner       *TestRunner
    coverageAnalyzer *CoverageAnalyzer
}

func (o *CoverageOptimizer) OptimizeCoverage(suite *TestSuite) (*TestSuite, error) {
    // 1. 执行当前测试
    results := o.testRunner.RunTestSuite(suite)
    
    // 2. 分析覆盖率
    coverageReport := o.coverageAnalyzer.Analyze(suite, results)
    
    // 3. 检查是否达标
    if o.isCoverageSufficient(coverageReport) {
        return suite, nil
    }
    
    // 4. 识别覆盖缺口
    gaps := o.identifyGaps(coverageReport)
    
    // 5. 生成补充测试
    newTests := o.generateTestsForGaps(gaps, suite)
    
    // 6. 合并测试
    mergedSuite := o.mergeTestSuites(suite, newTests)
    
    // 7. 递归优化
    return o.OptimizeCoverage(mergedSuite)
}

func (o *CoverageOptimizer) identifyGaps(report *CoverageReport) []CoverageGap {
    prompt := fmt.Sprintf(`
分析以下测试覆盖率报告，识别未覆盖的场景：

覆盖率报告:
%s

业务模型:
%s

请识别：
1. 哪些业务规则组合未被测试？
2. 哪些边界条件未被覆盖？
3. 哪些错误路径未被验证？
4. 可能的隐藏场景是什么？

对于每个缺口：
- 说明缺口的类型（规则组合/边界/错误/隐藏）
- 说明为什么这是重要场景
- 说明测试难度（简单/中等/复杂）

以JSON格式返回。
`, formatCoverageReport(report), formatBusinessModel(o.businessModel))
    
    response := o.llmClient.Call(prompt)
    return parseCoverageGaps(response)
}
```

### 4.2 迭代式测试生成

```go
func (o *CoverageOptimizer) generateTestsForGaps(gaps []CoverageGap, suite *TestSuite) *TestSuite {
    generator := &AITestGenerator{
        businessModel: o.businessModel,
        llmClient:     o.llmClient,
    }
    
    newSuite := &TestSuite{
        Function: suite.Function,
        Tests:    []*TestCase{},
    }
    
    for _, gap := range gaps {
        // 生成针对该缺口的测试
        tests := generator.generateTestsForGap(gap)
        newSuite.Tests = append(newSuite.Tests, tests...)
    }
    
    return newSuite
}
```

### 4.3 测试质量评估

```go
func (o *CoverageOptimizer) AssessTestQuality(suite *TestSuite) *QualityAssessment {
    prompt := fmt.Sprintf(`
评估以下测试套件的质量：

测试套件:
%s

请评估：
1. 断言质量（是否过弱/过强）
2. 测试独立性（是否有依赖）
3. 测试可读性（命名、注释）
4. 测试完整性（是否覆盖关键逻辑）
5. Flaky测试风险

对于每个问题：
- 识别具体的测试
- 说明问题所在
- 提供改进建议

以JSON格式返回。
`, formatTestSuite(suite))
    
    response := o.llmClient.Call(prompt)
    return parseQualityAssessment(response)
}
```

---

## 5. 工具辅助验证

### 5.1 验证架构

虽然AI主导，但工具仍扮演重要角色：

```go
// internal/validation/validator.go
type ValidationSuite struct {
    compileChecker  *CompileChecker
    staticAnalyzer  *StaticAnalyzer
    coverageRunner  *CoverageRunner
    flakyDetector   *FlakyDetector
}

func (v *ValidationSuite) ValidateAIResults(aiResults *AIResults) (*ValidationReport, error) {
    report := &ValidationReport{}
    
    // 1. 验证编译时错误
    compileErrors := v.compileChecker.Check(aiResults.ProjectPath)
    report.CompileErrors = compileErrors
    
    // 2. 验证静态分析发现
    staticIssues := v.staticAnalyzer.Check(aiResults.ProjectPath)
    report.StaticIssues = staticIssues
    
    // 3. 运行AI生成的测试
    testResults := v.coverageRunner.Run(aiResults.GeneratedTests)
    report.TestResults = testResults
    
    // 4. 检测Flaky测试
    flakyTests := v.flakyDetector.Detect(aiResults.GeneratedTests)
    report.FlakyTests = flakyTests
    
    // 5. 对比AI发现与工具发现
    report.Comparison = v.compareFindings(aiResults, report)
    
    return report, nil
}
```

### 5.2 工具的角色

```yaml
工具的职责:
  验证AI的发现:
    - 编译器验证AI发现的类型错误
    - 静态分析验证AI发现的复杂度问题
  
  执行AI生成的测试:
    - 运行测试并收集结果
    - 计算覆盖率指标
    - 检测Flaky测试
  
  提供客观指标:
    - 覆盖率百分比
    - 执行时间
    - 内存使用
  
  不做的:
    - 不做业务逻辑判断
    - 不做场景生成
    - 不做边界推断
```

---

## 6. CLI接口设计

### 6.1 主要命令

```bash
# AI主导的命令
cerberus ai understand           # 理解项目业务逻辑
cerberus ai generate-tests        # AI生成测试
cerberus ai optimize-coverage     # AI优化覆盖率
cerberus ai assess-quality        # AI评估测试质量

# 辅助命令
cerberus validate                 # 工具验证
cerberus report                   # 生成报告
cerberus status                   # 查看AI理解状态
```

### 6.2 最少交互式业务理解

```bash
$ cerberus ai understand

🤖 AI自主分析中...

✓ 深度代码分析: 45个文件, 12个模块
✓ 强化注释挖掘: 发现67个业务注释
✓ 业务模式识别: 识别5个业务模式
✓ AI推断业务模型

📊 AI推断结果:

业务领域: e-commerce (置信度: 85%)
  推断依据: 发现Order, Product, Payment等实体; 注释中出现"订单"、"支付"等术语

核心概念 (12个):
  ✓ Order (entity, 置信度: 90%) - 从类型定义和注释推断
  ✓ Product (entity, 置信度: 95%) - 从结构推断
  ✓ Discount (service, 置信度: 75%) - 从函数名推断
  ...

业务规则 (23个):
  ✓ 订单金额始终>0 (置信度: 95%) - 从验证代码推断
  ✓ VIP用户享受折扣 (置信度: 70%) - 从if语句推断，注释不完整
  ? 折扣上限40% (置信度: 45%) - 无法确定，可能需要确认
  ...

❓ AI需要确认1个关键问题:

1/1: 系统中是否有折扣上限？如果有，是多少？
   🤖 AI推断: 可能有40%上限 (置信度: 45%, 基于代码模式)
   💡 提示: 这将影响黑五+VIP+新用户的组合场景
   [默认: 40%] > 

✓ 使用默认推断: 40%上限

✓ 业务模型构建完成 (整体置信度: 82%)

📊 最终发现:
   - 12个核心业务概念
   - 23个业务规则 (19个高置信度, 4个中等)
   - 8个工作流
   - 15个边界场景

💾 已保存到: .cerberus/business_model.json

💡 下一步:
   生成测试: cerberus ai generate-tests
   查看详情: cerberus status
```

**关键特性:**
- AI先自主分析，展示所有推断结果和置信度
- 只询问1个关键问题（而不是5个）
- 提供AI推断作为默认值，用户直接回车即可接受
- 明确标注每个推断的置信度和依据

### 6.3 测试生成

```bash
$ cerberus ai generate-tests --function CalculateDiscount

🤖 AI正在生成测试...

✓ 分析函数签名和逻辑
✓ 加载相关业务规则
✓ 生成正常场景 (3个)
✓ 生成边界场景 (5个)
✓ 生成错误场景 (3个)
✓ 生成规则组合场景 (4个)

📝 生成了15个测试场景:

1. 正常-标准用户无折扣
2. 正常-VIP用户基础折扣
3. 正常-黑五全局折扣
4. 边界-折扣上限40%
5. 边界-零金额订单
6. 边界-最大金额VIP黑五
7. 边界-折扣精度计算
8. 错误-无效用户ID
9. 错误-负数金额
10. 错误-过期日期
11. 组合-黑五+VIP+新用户
12. 组合-折扣叠加顺序
13. 组合-VIP优先级验证
14. 组合-上限触发场景
15. 组合-多规则冲突

💾 已保存到: internal/service/discount_test.go

📊 预期覆盖率: 85%

💡 下一步:
   运行: cerberus ai optimize-coverage
   验证: cerberus validate
```

---

## 7. 配置管理

### 7.1 AI配置

```yaml
# .cerberus/ai_config.yaml
ai_config:
  # 模型配置
  model:
    name: "glm-5.1"
    temperature: 0.3
    max_tokens: 8000
  
  # 业务理解配置
  understanding:
    enabled: true
    auto_scan: true              # 自动扫描代码
    
    # 交互模式配置
    interaction_mode: "minimal"   # minimal | standard | interactive
    auto_infer_threshold: 0.6    # 置信度>此值不询问
    max_questions: 2             # 最多问题数量
    
    # AI推断配置
    extract_comments: true       # 提取注释语义（所有类型）
    aggressive_mining: true      # 强化注释挖掘（TODO/HACK/WARNING等）
    pattern_recognition: true     # 业务模式识别
    deep_code_analysis: true     # 深度代码分析
    
    # 存储配置
    save_model: true             # 保存业务模型
    include_confidence: true    # 包含置信度信息
  
  # 测试生成配置
  test_generation:
    max_scenarios: 50            # 每个函数最多生成50个场景
    include_combinations: true   # 包含规则组合
    include_edge_cases: true     # 包含边界场景
    target_coverage: 90.0       # 目标覆盖率90%
  
  # 覆盖优化配置
  coverage_optimization:
    enabled: true
    max_iterations: 5            # 最多5次迭代
    improvement_threshold: 5.0  # 每次至少提升5%
    time_limit: "10m"            # 最多10分钟
  
  # 成本控制
  cost_control:
    max_llm_calls: 100           # 每次最多100次LLM调用
    max_tokens_per_session: 100000
    budget_alert: true           # 超过预算时警告
```

### 7.2 业务模型存储

```json
// .cerberus/business_model.json
{
  "version": 1,
  "generated_at": "2026-06-16T10:30:00Z",
  "confidence": 0.82,
  "inference_source": "ai_autonomous",
  "domain": "e-commerce",
  "domain_confidence": 0.85,
  "concepts": [
    {
      "name": "Order",
      "type": "entity",
      "description": "订单实体，代表客户购买请求",
      "related_to": ["User", "Product", "Payment"],
      "inferred": true,
      "confidence": 0.95,
      "source": "code_structure"
    },
    {
      "name": "Discount",
      "type": "service",
      "description": "折扣计算服务，根据用户类型和活动应用折扣",
      "inferred": true,
      "confidence": 0.75,
      "source": "function_name_pattern"
    }
  ],
  "rules": [
    {
      "id": "order-amount-positive",
      "name": "订单金额必须为正数",
      "description": "订单金额始终必须大于0",
      "condition": "order.amount > 0",
      "effect": "validation_error",
      "priority": "critical",
      "source": "validation_code",
      "confidence": 0.95,
      "examples": [
        "amount = 100 ✓",
        "amount = 0 ✗",
        "amount = -50 ✗"
      ]
    },
    {
      "id": "vip-discount",
      "name": "VIP用户享受折扣",
      "description": "VIP用户享受基础10%折扣",
      "condition": "user.isVIP == true",
      "effect": "discount += 0.10",
      "priority": "high",
      "source": "if_statement_pattern",
      "confidence": 0.70,
      "examples": [
        "VIP用户: 10%折扣",
        "普通用户: 0%折扣"
      ]
    },
    {
      "id": "discount-max-cap",
      "name": "折扣上限40%",
      "description": "所有折扣总和不超过40%",
      "condition": "total_discount <= 0.40",
      "effect": "cap_discount_at_40_percent",
      "priority": "high",
      "source": "user_confirmed",
      "confidence": 1.0,
      "examples": [
        "黑五30% + VIP10% + 新用户5% = 40% (上限)",
        "单一折扣10% = 10%"
      ]
    }
  ],
  "edge_cases": [
    {
      "name": "折扣上限触发",
      "description": "多个折扣规则叠加时触发上限",
      "trigger": "黑五(30%) + VIP(10%) + 新用户(5%) = 45%",
      "expected": "折扣限制在40%",
      "rationale": "业务规则规定折扣上限为40%",
      "confidence": 1.0
    },
    {
      "name": "零金额订单",
      "description": "订单金额为零时的处理",
      "trigger": "order.amount == 0",
      "expected": "validation_error",
      "rationale": "订单金额必须为正数",
      "confidence": 0.95
    },
    {
      "name": "黑五+VIP+新用户组合",
      "description": "多个折扣规则同时触发时的上限逻辑",
      "trigger": "isBlackFriday && isVIP && isFirstOrder",
      "expected": "discount = 0.4 (上限)",
      "rationale": "30% + 10% + 5% = 45%，但上限为40%",
      "confidence": 0.90
    }
  ]
}
```
    {
      "name": "Discount",
      "type": "value_object",
      "description": "折扣计算逻辑",
      "related_to": ["Order", "User", "Promotion"]
    }
  ],
  "rules": [
    {
      "id": "discount_black_friday",
      "name": "黑五折扣规则",
      "description": "黑五期间所有订单享受30%折扣",
      "condition": "order.date.isBlackFriday() == true",
      "effect": "discount += 0.3",
      "priority": "high"
    },
    {
      "id": "discount_vip",
      "name": "VIP用户折扣",
      "description": "VIP用户享受额外10%折扣",
      "condition": "order.user.isVIP == true",
      "effect": "discount += 0.1",
      "priority": "medium"
    },
    {
      "id": "discount_limit",
      "name": "折扣上限",
      "description": "总折扣不超过40%",
      "condition": "discount > 0.4",
      "effect": "discount = 0.4",
      "priority": "critical"
    }
  ],
  "workflows": [
    {
      "name": "order_processing",
      "steps": [
        "Create Order",
        "Calculate Discount",
        "Process Payment",
        "Update Inventory",
        "Ship Order"
      ],
      "entry_points": ["Create Order"],
      "exit_points": ["Order Completed", "Order Cancelled"]
    }
  ],
  "invariants": [
    {
      "description": "订单金额必须大于0",
      "expression": "order.amount > 0",
      "scope": "Order"
    },
    {
      "description": "折扣后价格不超过原价",
      "expression": "order.finalPrice <= order.originalPrice",
      "scope": "Order"
    }
  ],
  "edge_cases": [
    {
      "name": "黑五+VIP+新用户组合",
      "description": "多个折扣规则同时触发时的上限逻辑",
      "trigger": "isBlackFriday && isVIP && isFirstOrder",
      "expected": "discount = 0.4 (上限)",
      "rationale": "30% + 10% + 5% = 45%，但上限为40%"
    }
  ]
}
```

---

## 8. 实施路线图

### Phase 1: AI自主业务理解 (4周)

**目标:** 实现AI自主业务理解引擎（最少交互）

**交付物:**
- ✅ 深度代码分析器
- ✅ 强化注释挖掘器（所有注释类型）
- ✅ 业务模式识别器
- ✅ AI综合推断引擎
- ✅ 最少交互控制器
- ✅ 业务模型持久化（含置信度）

**验证:**
- AI能自主推断业务领域（置信度>80%）
- AI能从注释中提取业务规则（召回率>70%）
- 只在必要时询问1-2个问题
- 业务模型准确率 > 75%

**关键特性:**
- 挖掘所有类型注释（单行、多行、TODO、HACK、WARNING等）
- 业务模式识别（工作流、状态机、规则模式）
- 置信度驱动的交互策略
- 每个推断都标注置信度和依据

### Phase 2: AI测试生成 (3周)

**目标:** 实现AI测试生成引擎

**交付物:**
- ✅ 场景生成器（正常/边界/错误/组合）
- ✅ 测试代码生成
- ✅ 多语言支持（Go/Node/Python）
- ✅ 业务规则组合引擎

**验证:**
- AI生成的测试能运行（编译通过率>90%）
- 场景覆盖率 > 85%
- 测试代码质量可接受

### Phase 3: AI覆盖优化 (2周)

**目标:** 实现AI迭代优化

**交付物:**
- ✅ 覆盖缺口分析（基于业务模型）
- ✅ 迭代测试生成
- ✅ 质量评估

**验证:**
- 能将覆盖率提升到目标水平
- 优化迭代次数 < 5次

### Phase 4: 工具集成 (1周)

**目标:** 集成工具验证

**交付物:**
- ✅ 编译检查集成
- ✅ 静态分析集成
- ✅ 覆盖率工具集成

**总计:** 10周

---

## 9. 风险和限制

### 已知限制

1. **AI自主推断的准确性**
   - 依赖代码质量和注释完整性
   - 可能误理解复杂业务逻辑
   - 对于隐性业务规则可能推断失败
   - 置信度评估本身可能不准确

2. **强化注释挖掘的挑战**
   - 不是所有代码都有充分的注释
   - TODO/HACK/WARNING注释可能包含过时信息
   - 注释与代码实现可能不一致
   - 需要区分业务注释和技术注释

3. **最少交互的权衡**
   - 减少交互可能降低准确性
   - 默认推断值可能不正确
   - 用户可能不了解AI推断的依据
   - 需要设计好的置信度展示机制

4. **AI生成测试的质量**
   - 可能包含编译错误
   - 断言可能不准确（基于错误的业务理解）
   - Mock和Stub可能不完整
   - 需要人工审查

5. **成本考虑**
   - LLM调用有成本
   - 强化注释挖掘增加调用次数
   - 需要设置合理预算
   - 需要监控使用量

### 风险缓解

| 风险 | 缓解措施 |
|------|----------|
| AI推断不准确 | 最少交互兜底 + 置信度展示 + 人工审查 |
| 注释挖掘质量差 | 多源验证（代码+注释+执行）+ 置信度加权 |
| 默认值错误 | 清晰展示推断依据 + 允许用户修改 |
| AI生成测试质量差 | 工具验证 + 编译检查 + 人工审查 |
| LLM成本过高 | 设置调用限制 + 缓存机制 + 智能去重 |
| 依赖外部服务 | 支持本地模型部署 |

### 成功指标

| 指标 | 目标 | 测量方式 |
|------|------|----------|
| AI推断准确率 | >75% | 人工验证业务模型 |
| 交互问题数 | ≤2次/项目 | 统计用户交互次数 |
| 测试编译通过率 | >90% | 自动编译检查 |
| 覆盖率提升 | >20% | 覆盖率报告对比 |
| 用户满意度 | >80% | 用户反馈调查 |

---

## 10. 成功指标

### 技术指标

- ✅ AI业务理解准确率 > 70%
- ✅ AI生成测试可运行率 > 90%
- ✅ 场景覆盖率 > 85%
- ✅ 覆盖优化提升 > 30%

### 用户指标

- ✅ 业务理解时间 < 10分钟
- ✅ 测试生成时间 < 5分钟/函数
- ✅ 用户满意度 > 80%

### 成本指标

- ✅ 每个项目LLM调用 < 100次
- ✅ 每月Token消耗 < 1M
- ✅ 单次完整分析成本 < $10

---

## 附录

### A. 术语表

- **Business Model（业务模型）**: AI对项目业务逻辑的理解表示
- **Scenario（场景）**: 一个完整的测试用例描述
- **Coverage Gap（覆盖缺口）**: 未被测试覆盖的业务场景
- **Edge Case（边界场景）**: 参数或状态的边界情况

### B. 参考资源

- AI测试生成: [TestGen-LLM](https://arxiv.org/abs/2305.12674)
- 业务理解: [CodeLlama](https://github.com/facebookresearch/codellama)
- 覆盖优化: [Coverage-Guided-Testing](https://ieeexplore.ieee.org/document/9458276)

### C. 变更历史

| 版本 | 日期 | 变更说明 |
|------|------|----------|
| 2.1 | 2026-06-16 | 更新为"AI主导+最少交互"架构 - 强化AI自主推断，最小化交互 |
| 2.0 | 2026-06-16 | 转向AI主导架构 - AI主动分析，交互式问答补充 |
| 1.0 | 2026-06-16 | 初始版本 - 工具辅助为主的设计 |

---

## 11. 总结

### 核心理念

本设计文档描述了Cerberus质量检查框架的v2.1版本，采用**"AI主导+最少交互"**的架构：

1. **AI优先推断**：AI通过深度代码分析、强化注释挖掘、业务模式识别，自主推断业务模型
2. **最少交互**：只在AI置信度低时（<60%）才询问，每次最多1-2个问题
3. **置信度驱动**：每个推断都标注置信度和依据，明确告知不确定性
4. **诚实告知**：不编造信息，不确定时明确标记

### 关键创新

- **强化注释挖掘**：挖掘所有类型注释（单行、多行、TODO、HACK、WARNING等），最大限度从代码中提取业务信息
- **业务模式识别**：自动识别工作流模式、状态机模式、规则模式，从代码结构中推断业务逻辑
- **渐进式精确化**：第一轮AI完全自主分析，第二轮针对低置信度部分询问，后续根据测试结果迭代优化
- **用户友好**：提供AI推断作为默认值，用户可直接接受或修改，允许跳过问题

### 下一步行动

1. **审查设计文档**：用户审查本设计文档，确认架构方向
2. **创建实施计划**：基于设计文档创建详细的实施计划
3. **开始Phase 1开发**：实现AI自主业务理解引擎

---

**文档状态:** 待审查
**文档版本:** v2.1
**最后更新:** 2026-06-16
**下一步:** 等待用户审查和批准
