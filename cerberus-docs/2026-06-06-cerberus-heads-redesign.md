# 设计：Cerberus 三头架构重设计

> 日期: 2026-06-06
> 状态: 设计
> 前置: review 反馈指出 Explorer 承担系统 75% 复杂度，Checker 能力过弱
> 变更: 从"能力型划分"（Explorer/Judge/Checker）改为"生命周期型划分"（Scout/Agent/Examiner）

## 0. 为什么重新设计

### 原架构问题

原三头按**能力类型**划分：

| 头 | 包含的子模块 | 复杂度占比 |
|---|-------------|-----------|
| Explorer | 代码分析 + 爬取 + DB schema + 浏览器操作 + 测试规划 + 操作执行 + 失败恢复 + 外部知识源 | **75%** |
| Judge | AI 判断 + 置信度策略 | 15% |
| Checker | SQL 查询 + Go 函数 | 10% |

Explorer 变成了**杂物桶**——所有不属于"判断"和"检查"的功能都塞给它。"分析代码"和"点击浏览器按钮"是完全不同的研究领域的活动，却放在同一个头里。

### 新架构原则

1. **按生命周期阶段划分**：每个头对应测试流程的一个阶段
2. **每个头有单一职责**：能一句话说清楚它做什么
3. **输入输出明确**：每个头产出一个明确的数据结构
4. **复杂度均衡**：每个头 3-6 个子模块

## 1. 新三头定义

```
                    ┌─────────────────────┐
                    │     Session          │
                    │  run / verify / serve │
                    └────────┬────────────┘
                             │
                    ┌────────┴────────┐
                    │    AI Driver     │  ← 统一 AI 调用入口
                    │  Token Budget    │    （预算管理、降级）
                    │  结构化输出解析    │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
     ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
     │   Scout     │ │   Agent     │ │  Examiner   │
     │  (侦察头)    │ │  (执行头)    │ │  (审查头)    │
     └──────┬──────┘ └──────┬──────┘ └──────┬──────┘
            │               │               │
      PM + TestPlan      Evidence[]       Verdict[]
            │               │               │
            ▼               ▼               ▼
     ┌──────────────────────────────────────────┐
     │        EvidenceStore + Memory System     │
     └──────────────────────────────────────────┘
```

数据流：**Scout → Agent → Examiner**（线性管道，各阶段独立）

| 头 | 核心问题 | 输入 | 输出 | AI 决策点 |
|---|---------|------|------|----------|
| **Scout**（侦察头） | "这个项目长什么样？" | URL + 代码路径 + DB 连接 | ProjectModel + TestPlan | 认知、规划 |
| **Agent**（执行头） | "按计划执行，采集证据" | TestPlan | Evidence[] | 执行引导、恢复 |
| **Examiner**（审查头） | "测试结果是否正确？" | Evidence[] + 期望描述 | Verdict[] + 记忆更新 | 判断、学习 |

## 2. Scout（侦察头）详解

### 职责

理解项目结构，规划测试方案。**不做任何测试执行**。

### 流程

```
目标 URL + 可选代码路径 + 可选 DB 连接
  │
  ├─ 并行三维度扫描
  │   ├─ Surface：HTTP client 爬取页面 → 页面列表、导航流
  │   ├─ Code：patternscan/openapi/manifest → API 端点、技术栈
  │   └─ Data：information_schema → 表关系、约束
  │
  ├─ 可选增强（零依赖降级）
  │   └─ 外部知识源：cccmemory / codegraph（Available() → false 则跳过）
  │
  ├─ AI 认知推理（决策点：认知）
  │   └─ 从扫描结果推理完整项目模型
  │      例："发现 /admin/users + users 表 → 推理存在 CRUD API"
  │
  └─ 测试规划（决策点：规划）
      └─ 基于项目模型 + 目标 + 记忆 → TestPlan
         按 confidence 升序排序 → 低置信度的推理优先验证
```

### 接口

```go
// head/scout/scout.go
type Scout interface {
    // 认知：扫描 + 推理 → 项目模型
    Analyze(ctx context.Context, target TargetInfo) (*ProjectModel, error)
    // 规划：基于模型生成测试计划
    Plan(ctx context.Context, goal string, model *ProjectModel) (*TestPlan, error)
}
```

### 包结构

```
internal/head/scout/
├── scout.go          # Scout 接口 + 协调三维度扫描
├── recon.go          # 并行三维度扫描（Surface + Code + Data）
├── analyzer.go       # AI 认知推理（合并线索 → ProjectModel）
├── planner.go        # 测试规划（元认知：优先级、排序、覆盖度门控）
├── knowledge.go      # 外部知识源接口（可选增强）
└── code/             # 内置代码分析器
    ├── patternscan.go
    ├── openapi.go
    ├── manifest.go
    └── configscan.go
```

## 3. Agent（执行头）详解

### 职责

按 TestPlan 执行测试步骤，采集证据。**不做任何判断**。

### 执行策略

```
TestPlan 中的每个 Step：
  │
  ├─ 规则引擎优先处理（零 AI 调用，零延迟）
  │   ├─ 步骤指定了 selector + action → 直接执行
  │   ├─ 页面 URL 匹配预期 → 执行预设操作
  │   └─ 表单字段有 label 匹配 → 自动填充
  │
  ├─ 需要 AI 介入（决策点：执行引导）
  │   ├─ 页面结构与预期不符（新弹窗、结构变化）
  │   ├─ 操作失败且非网络错误
  │   └─ 步骤描述模糊
  │
  └─ 失败恢复（决策点：恢复）
      └─ 重试 / 换路径 / 跳过（按需，每步骤最多 3 次）
```

### 证据采集

每步采集：
- API 测试：HTTP 请求 + 响应（status + headers + body）
- UI 测试：截图 + 页面 snapshot
- DB 测试：操作前后的 table snapshot（before/after diff）

### 接口

```go
// head/agent/agent.go
type Agent interface {
    // 执行测试计划，采集证据
    Execute(ctx context.Context, plan *TestPlan, actors []Actor) ([]Evidence, error)
}
```

### 包结构

```
internal/head/agent/
├── agent.go          # Agent 接口 + 步骤调度
├── executor.go       # 规则引擎 + AI 引导决策树
├── api.go            # API 测试执行（HTTP client）
├── browser/          # 浏览器测试执行
│   ├── mcp_client.go # Playwright MCP 通信层
│   ├── actions.go    # click/type/navigate 封装
│   └── snapshot.go   # 页面快照 + 截图
├── recovery.go       # 失败恢复策略
└── evidence.go       # 证据采集（before/after diff、截图、响应）
```

## 4. Examiner（审查头）详解

### 职责

评估证据，产出 Verdict。**合并原 Judge 和 Checker**，内部路由到不同评估方法。

### 评估路由

```
每条 Evidence + 期望描述：
  │
  ├─ 有确定性检查定义？→ 路由到对应 Checker（零 AI）
  │   ├─ SQL 查询 + assertion → SQL Checker
  │   ├─ HTTP 断言（status/header/body）→ HTTP Checker
  │   ├─ DB before/after diff → Diff Checker
  │   ├─ JSON Schema 验证 → Schema Checker（可选依赖）
  │   └─ Go 函数 → Plugin Checker
  │
  ├─ 无确定性检查 或 Checker 无法覆盖 → AI Judge（决策点：判断）
  │   └─ 证据 + 期望 → LLM → Verdict（pass/fail/uncertain + confidence）
  │
  ├─ 同一目标有多种结果 → 裁决合并
  │   └─ 确定性结果 > AI 结果（Checker > Judge）
  │
  └─ Uncertain 处理（3 级降级链）
      ├─ L1：自动重试（换 prompt + 收集更多证据）
      ├─ L2：降级为 Checker-only（如无 Checker → 跳到 L3）
      └─ L3：标记待审（写入 pending-review.yaml）
```

### 学习（决策点：学习）

Session 结束时：
- 写入 L1 情景记忆（测试事件）
- 提炼写入 L2 语义记忆（新发现的知识）
- 更新 L3 程序记忆（策略有效性）
- 校正 ProjectModel 的置信度

### 接口

```go
// head/examiner/examiner.go
type Examiner interface {
    // 评估证据，产出裁决
    Examine(ctx context.Context, evidence []Evidence, plan *TestPlan) ([]Verdict, error)
    // Session 结束时学习
    Learn(ctx context.Context, verdicts []Verdict) error
}
```

### 包结构

```
internal/head/examiner/
├── examiner.go       # 检查路由 + 裁决合并
├── judge.go          # AI 判断（LLM 调用 + 结构化输出）
├── learner.go        # 学习逻辑（更新 L1/L2/L3 记忆 + ProjectModel）
├── policy.go         # 置信度策略 + Uncertain 3 级降级链
├── checker/          # 确定性检查器
│   ├── checker.go    # 注册表 + 类型路由
│   ├── assertion.go  # 通用断言解析器（==, !=, contains, is_array）
│   ├── sql.go        # SQL Checker
│   ├── http.go       # HTTP Checker（status/header/body/json_path）
│   ├── diff.go       # Diff Checker（DB before/after）
│   └── enhanced/     # 可选增强
│       ├── jsonschema.go  # JSON Schema（gojsonschema）
│       └── openapi.go     # OpenAPI 契约（kin-openapi）
└── types.go          # Verdict, CheckResult, CheckDef 等公共类型
```

## 5. 与原架构的映射

| 原模块 | 原归属 | 新归属 | 说明 |
|--------|--------|--------|------|
| Recon（代码分析 + 爬取 + schema） | Explorer | **Scout** | 职责不变 |
| AI 认知推理 | Explorer | **Scout** | 职责不变 |
| 测试规划 | Explorer | **Scout** | 从 Explorer 移出 |
| 外部知识源 | Explorer | **Scout** | 职责不变 |
| 浏览器操作 | Explorer | **Agent** | 从 Explorer 移出 |
| 操作执行 + 规则引擎 | Explorer | **Agent** | 从 Explorer 移出 |
| 失败恢复 | Explorer | **Agent** | 从 Explorer 移出 |
| 证据采集 | Explorer | **Agent** | 从 Explorer 移出 |
| AI 判断 | Judge | **Examiner** | 合并 |
| 置信度策略 | Judge | **Examiner** | 合并 |
| SQL/HTTP/Diff 检查 | Checker | **Examiner/checker/** | 合并为子模块 |
| 裁决合并 | Arbitrator | **Examiner** | 从独立模块降为内部逻辑 |
| 学习 | 无 | **Examiner** | 新增为显式职责 |

## 6. AI 决策点分配

| 决策点 | 归属头 | 说明 |
|--------|--------|------|
| 认知 `Analyze` | Scout | 扫描结果 → ProjectModel |
| 规划 `Plan` | Scout | ProjectModel → TestPlan |
| 执行引导 `Steer` | Agent | 页面异常时决定下一步 |
| 恢复 `Recover` | Agent | 操作失败时决定新路径 |
| 判断 `Judge` | Examiner | 证据 + 期望 → Verdict |
| 学习 `Learn` | Examiner | Verdict → 记忆更新 |

Token 消耗因项目规模和复杂度而异，不做固定估算。每个决策点的调用频率：
- **认知**：每 session 1-3 次（增量运行时 1 次）
- **规划**：每 session 1-2 次
- **执行引导**：按需（~30% 的操作需要 AI 介入）
- **恢复**：按需（失败时触发，每步骤最多 3 次）
- **判断**：每条证据 1-2 次
- **学习**：每 session 1 次

## 7. `run` 命令流程（更新后）

```
cerberus run --url URL --goal "目标"

Step 0: 项目识别
  └─ 加载 .cerberus/project.yaml + project-model.yaml + 记忆

Step 1-2: Scout 阶段
  ├─ Recon：三维度扫描（Surface + Code + Data）
  ├─ Analyze：AI 认知推理 → ProjectModel
  └─ Plan：生成 TestPlan

Step 3-4: Agent 阶段
  ├─ Execute：按 TestPlan 执行（规则引擎 + AI 引导）
  └─ Collect：采集 Evidence[]

Step 5-6: Examiner 阶段
  ├─ Examine：确定性检查 + AI 判断 → Verdict[]
  ├─ Arbitrate：裁决合并（确定性 > AI）
  └─ Learn：更新记忆 + 校正 ProjectModel
```

## 8. 阶段规划更新

```
C1 ──→ C1.5 ──→ C2a ──→ C2b ──→ C3 ──→ C4 ──→ C5 ──→ C6
(骨架)  (验证)    (Scout) (Agent  (Examiner)(记忆)  (会话)  (Server)
                  API     浏览器)
                          ↑                    ↑
                          └─── MVP ───────────┘
```

| 阶段 | 内容 | 依赖 | 工期 | 风险 |
|------|------|------|------|------|
| C1 | 骨架 + CLI + 项目插件 + LLM Client + AI Driver | 无 | 3 周 | 低 |
| C1.5 | **核心假设验证**：Judge 准确率 ≥ 85% | C1 | 1 周 | **关键** |
| C2a | **Scout 头**：三维度扫描 + AI 认知推理 + 测试规划 | C1.5 | 2 周 | 低 |
| C2b | **Agent 头（浏览器）**：Playwright MCP + 规则引擎 | C2a | 2 周 | **高** |
| C3 | **Examiner 头**：AI 判断 + 确定性检查 + 裁决 + 学习 | C2a | 2 周 | 中 |
| C4 | 记忆系统（L1+L2+L3）+ SQLite → PostgreSQL | C1 | 2.5 周 | 中 |
| C5 | 会话管理 + EvidenceStore + 报告 | C1 | 2 周 | 低 |
| C6 | Server + 集成测试 + 文档 | C3+C4+C5 | 2 周 | 低 |
