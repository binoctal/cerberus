# Architecture Quality Check Design

**Date**: 2026-06-16  
**Status**: Design Phase  
**Related**: Issue #30 (架构质量检查机制)

## Problem Statement

当前 Cerberus 只能检查**功能正确性**，无法检查**架构质量**。

**实际案例**：
- ❌ `internal/runtime/paths.go` 从 62 行膨胀到 157 行（过度工程化）
- ❌ 支持 4 个 OS + Docker（不必要的复杂性）
- ❌ 缺少使用场景分析（假设所有用户需要系统级安装）

这些问题**在实现阶段才发现**，应该在**设计阶段**就被发现。

## Goal

让 Cerberus 能够**自动检查架构设计问题**：

1. **过度工程化检测**
2. **场景假设验证**
3. **简单性原则检查**
4. **依赖关系分析**
5. **架构模式识别**

## Architecture Issue Taxonomy

### 1. Over-engineering（过度工程化）

**定义**：实现超出实际需求的复杂性

**示例**：
- `paths.go` 157 行，支持 4 个 OS + Docker
- 实际只需要：项目目录存储（62 行）

**检测指标**：
```go
// 复杂度阈值
const (
    MaxLinesPerFile      = 150   // 单文件最大行数
    MaxFunctionsPerFile  = 15    // 单文件最大函数数
    MaxNestingDepth      = 4     // 最大嵌套深度
    MaxParameters       = 5     // 函数最大参数数
    MaxCyclomaticComplexity = 10 // 圈复杂度
)
```

**检查规则**：
```yaml
invariants:
  - id: avoid-over-engineering
    description: "避免过度复杂的实现"
    check: "complexity_analysis(source_file) < threshold"
    severity: "warning"
    rationale: "简单代码更容易维护和测试"
```

### 2. Missing Scenario Analysis（缺少场景分析）

**定义**：在未分析使用场景的情况下做出架构决策

**示例**：
- 假设所有用户需要系统级安装
- 实际：大多数用户只需项目本地运行

**检测方法**：
```go
// 场景覆盖率检查
type ScenarioCoverage struct {
    DevScenarios      []string // 开发场景
    UserScenarios     []string // 用户场景
    CI/CDScenarios    []string // CI/CD场景
    CoveredScenarios  []string // 已覆盖场景
}

// 检查：是否有未文档化的场景假设
func (a *ArchitectureAnalyzer) CheckScenarioAssumptions(decision ArchitectureDecision) []Issue {
    issues := []Issue{}
    
    // 检查是否有 ADR（架构决策记录）
    if !hasADR(decision) {
        issues = append(issues, Issue{
            Type:        "missing_scenario_analysis",
            Description: "架构决策缺少场景分析文档",
            Severity:    "warning",
            Suggestion:  "创建 ADR 记录决策理由和使用场景",
        })
    }
    
    // 检查是否分析了替代方案
    if !hasAlternativesConsidered(decision) {
        issues = append(issues, Issue{
            Type:        "missing_alternatives",
            Description: "未分析替代方案",
            Severity:    "info",
            Suggestion:  "记录考虑过的方案和拒绝理由",
        })
    }
    
    return issues
}
```

### 3. Premature Abstraction（过早抽象）

**定义**：在没有实际需求的情况下创建抽象层

**示例**：
- 创建接口"为了未来可能需要"
- 实现 5 个平台支持"以防万一"

**检测方法**：
```go
// 检查抽象使用情况
func (a *ArchitectureAnalyzer) CheckUnusedAbstractions(codebase *Codebase) []Issue {
    issues := []Issue{}
    
    // 检查接口实现数量
    for _, iface := range codebase.Interfaces {
        if len(iface.Implementations) == 1 {
            issues = append(issues, Issue{
                Type:        "premature_abstraction",
                Description: fmt.Sprintf("接口 %s 只有 1 个实现", iface.Name),
                Severity:    "info",
                Rationale:   "单一实现的接口可能是过早抽象",
                Suggestion:  "考虑在真正需要多实现时再抽象",
            })
        }
    }
    
    // 检查未使用的抽象层
    for _, layer := range codebase.Layers {
        if layer.UsageCount == 0 {
            issues = append(issues, Issue{
                Type:        "unused_abstraction",
                Description: fmt.Sprintf("抽象层 %s 未被使用", layer.Name),
                Severity:    "warning",
                Suggestion:  "删除未使用的抽象",
            })
        }
    }
    
    return issues
}
```

### 4. Violating SOLID Principles

**定义**：违反 SOLID 原则的架构设计

**检测指标**：
```go
// SRP（单一职责原则）检查
func (a *ArchitectureAnalyzer) CheckSRP(file *SourceFile) []Issue {
    issues := []Issue{}
    
    // 检查文件是否有多个职责
    responsibilities := a.identifyResponsibilities(file)
    if len(responsibilities) > 1 {
        issues = append(issues, Issue{
            Type:        "violates_srp",
            Description: fmt.Sprintf("文件 %s 有 %d 个职责", file.Name, len(responsibilities)),
            Responsibilities: responsibilities,
            Severity:    "warning",
            Suggestion:  "考虑拆分为多个文件，每个文件一个职责",
        })
    }
    
    return issues
}

// 识别文件的职责
func (a *ArchitectureAnalyzer) identifyResponsibilities(file *SourceFile) []string {
    // 基于函数名、注释、结构体名识别职责
    responsibilities := map[string]bool{}
    
    for _, fn := range file.Functions {
        // 提取职责关键词
        if contains(fn.Name, "parse") || contains(fn.Name, "read") {
            responsibilities["parsing"] = true
        }
        if contains(fn.Name, "validate") || contains(fn.Name, "check") {
            responsibilities["validation"] = true
        }
        if contains(fn.Name, "persist") || contains(fn.Name, "save") {
            responsibilities["persistence"] = true
        }
        // ... 更多职责类型
    }
    
    return keys(responsibilities)
}
```

### 5. Circular Dependencies（循环依赖）

**定义**：模块之间存在循环依赖

**检测方法**：
```go
// 循环依赖检测
func (a *ArchitectureAnalyzer) DetectCircularDependencies(graph *DependencyGraph) []Issue {
    issues := []Issue{}
    
    // 使用 DFS 检测环
    visited := make(map[string]bool)
    recStack := make(map[string]bool)
    
    for node := range graph.Nodes {
        if !visited[node] {
            if a.hasCycle(node, visited, recStack, graph) {
                cycle := a.extractCycle(recStack, graph)
                issues = append(issues, Issue{
                    Type:        "circular_dependency",
                    Description: "检测到循环依赖",
                    Cycle:       cycle,
                    Severity:    "error",
                    Rationale:   "循环依赖导致难以理解和测试",
                    Suggestion:  "引入依赖倒置或提取共同依赖",
                })
            }
        }
    }
    
    return issues
}

func (a *ArchitectureAnalyzer) hasCycle(node string, visited, recStack map[string]bool, graph *DependencyGraph) bool {
    visited[node] = true
    recStack[node] = true
    
    for _, neighbor := range graph.Nodes[node] {
        if !visited[neighbor] {
            if a.hasCycle(neighbor, visited, recStack, graph) {
                return true
            }
        } else if recStack[neighbor] {
            return true
        }
    }
    
    recStack[node] = false
    return false
}
```

## Implementation Design

### Package Structure

```
internal/
├── architecture/
│   ├── analyzer.go          # 主分析引擎
│   ├── complexity.go        # 复杂度分析
│   ├── scenarios.go         # 场景分析
│   ├── abstraction.go       # 抽象分析
│   ├── solid.go            # SOLID 原则检查
│   ├── dependencies.go     # 依赖分析
│   └── issues.go           # Issue 类型定义
├── validation/
│   └── architecture_validator.go  # 架构验证器
```

### Core Types

```go
// ArchitectureIssue represents an architecture problem
type ArchitectureIssue struct {
    ID          string   // 唯一标识
    Type        string   // overengineering | missing_scenario | circular_dependency etc
    Severity    string   // error | warning | info
    File        string   // 问题文件
    Line        int      // 问题行号
    Description string   // 问题描述
    Rationale   string   // 为什么这是问题
    Suggestion  string   // 如何修复
    Confidence  float64 // AI推断置信度（如果适用）
    Evidence    []string // 证据（代码片段、度量等）
}

// ArchitectureReport contains analysis results
type ArchitectureReport struct {
    ProjectPath    string
    AnalyzedAt     time.Time
    Issues         []ArchitectureIssue
    Metrics        *ArchitectureMetrics
    Recommendations []string
}

// ArchitectureMetrics represents code quality metrics
type ArchitectureMetrics struct {
    // 复杂度指标
    TotalFiles        int
    TotalLines        int
    AvgLinesPerFile   int
    MaxLinesPerFile   int
    
    // 函数指标
    TotalFunctions    int
    AvgFunctionsPerFile int
    MaxParameters     int
    MaxNestingDepth   int
    
    // 依赖指标
    TotalDependencies int
    CircularDependencies int
    
    // 抽象指标
    TotalInterfaces   int
    UnusedAbstractions int
    
    // SOLID 违规
    SRPViolations    int
    OCPViolations    int
    LSPViolations    int
    ISPViolations    int
    DIPViolations    int
}
```

### Integration with Quality Check Framework

在现有的 invariants 中加入架构检查：

```yaml
# .cerberus/project.yaml
invariants:
  # === 架构质量检查 ===
  
  # 复杂度检查
  - id: max_file_complexity
    description: "单文件不超过 150 行"
    check: "architecture.max_lines_per_file() <= 150"
    severity: "warning"
    
  # 过度抽象检查
  - id: avoid_unused_abstractions
    description: "避免未使用的抽象"
    check: "architecture.unused_abstractions() == 0"
    severity: "info"
    
  # 循环依赖检查
  - id: no_circular_dependencies
    description: "不允许循环依赖"
    check: "architecture.circular_dependencies() == 0"
    severity: "error"
    
  # SRP 检查
  - id: single_responsibility_per_file
    description: "每个文件单一职责"
    check: "architecture.responsibilities_per_file() <= 1"
    severity: "warning"
    
  # 场景分析检查
  - id: document_architecture_decisions
    description: "重要架构决策需要有 ADR 文档"
    check: "architecture.has_adr_for_changes() == true"
    severity: "info"
```

## Usage Example

### CLI 命令

```bash
# 检查架构质量
cerberus check architecture

# 检查特定文件
cerberus check architecture --file internal/runtime/paths.go

# 生成架构报告
cerberus check architecture --report architecture-report.md
```

### 输出示例

```
🏗️  架构质量检查报告

📊 代码度量:
  总文件数: 45
  总代码行数: 8,234
  平均行数/文件: 183
  最大行数/文件: 312 ⚠️

⚠️  发现 3 个架构问题:

1. [warning] 过度复杂 (internal/runtime/paths.go)
   文件有 157 行，超过阈值 150 行
   证据: 
     - 5 个平台特定函数 (Linux, macOS, Windows, Docker, Development)
     - 1 个 Docker 环境检测函数
   建议: 考虑简化实现，使用项目目录存储（62 行）
   
2. [info] 可能的过早抽象 (internal/runtime/paths.go)
   接口 RuntimePathsProvider 只有 1 个实现
   建议: 如果只有单一实现，考虑在真正需要多实现时再抽象
   
3. [error] 循环依赖 (internal/auth/session.go → internal/user/service → internal/auth/manager)
   依赖环: session.go → service.go → manager.go → session.go
   建议: 引入依赖倒置或提取共同依赖到独立包

💡 改进建议:
1. 简化 runtime/paths.go，移除不必要的平台支持
2. 将单一实现的接口改为具体类型
3. 重构 auth 包，消除循环依赖

✓ 架构健康度: 65/100
  复杂度: 60/100 ⚠️
  简单性: 55/100 ⚠️
  可维护性: 80/100 ✓
```

## Implementation Tasks

### Phase 1: Core Infrastructure

1. **创建架构分析包** (`internal/architecture/`)
   - 定义核心类型
   - 实现基础框架

2. **实现复杂度分析器** (`complexity.go`)
   - 文件行数统计
   - 函数参数计数
   - 嵌套深度分析
   - 圈复杂度计算

3. **实现依赖分析器** (`dependencies.go`)
   - 构建依赖图
   - 检测循环依赖
   - 分析耦合度

### Phase 2: Pattern Recognition

4. **实现抽象分析器** (`abstraction.go`)
   - 检测未使用的抽象
   - 识别过早抽象
   - 分析接口使用情况

5. **实现 SOLID 检查器** (`solid.go`)
   - SRP 检查（单一职责）
   - OCP 检查（开闭原则）
   - 其他 SOLID 原则

### Phase 3: Scenario Analysis

6. **实现场景分析器** (`scenarios.go`)
   - 检测架构决策文档
   - 分析场景覆盖
   - 识别未记录的假设

### Phase 4: Integration

7. **集成到质量检查框架**
   - 添加架构 invariants
   - 集成到 validator
   - 生成架构报告

8. **实现 CLI 命令**
   - `cerberus check architecture`
   - 报告生成
   - 可视化输出

## Success Criteria

✅ **能够检测出已知问题**：
- 检测 `paths.go` 的 157 行复杂度问题
- 识别 4 个 OS + Docker 的过度工程化
- 发现缺少场景分析的架构决策

✅ **提供可操作的改进建议**：
- 不是简单报告问题
- 给出具体修复步骤
- 说明为什么这样建议

✅ **易于集成到现有工作流**：
- 作为 invariant 配置
- CLI 命令快速检查
- CI/CD 集成

## References

- Original discussion: runtime/paths.go over-engineering (157 → 62 lines)
- ADR template: [Architecture Decision Records](https://adr.github.io/)
- SOLID principles: [Agile Software Development](https://en.wikipedia.org/wiki/SOLID)
- Complexity metrics: [Cyclomatic Complexity](https://en.wikipedia.org/wiki/Cyclomatic_complexity)
