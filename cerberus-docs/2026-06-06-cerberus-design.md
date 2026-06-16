# 设计: Cerberus — 通用 AI 测试框架

> 日期: 2026-06-06
> 状态: 设计 (v3 — 三头重设计)
> 架构变更: v2 的 Explorer/Judge/Checker（能力型划分）→ v3 的 Scout/Agent/Examiner（生命周期型划分）
> 详见: `docs/2026-06-06-cerberus-heads-redesign.md`

## 0. 项目定位

**Cerberus** 是面向 SaaS 开发者的通用 AI 测试框架。**框架用 Go 实现，但测试目标不限语言**——Go/Python/Java/Node/Rust/PHP 项目均可测试。名字来源于希腊神话中守护冥界的三头犬——三头对应测试生命周期的三个阶段：

| 头 | 名称 | 阶段 | 核心问题 |
|----|------|------|----------|
| Head 1 | Scout（侦察头） | 认知 + 规划 | "这个项目长什么样？该测什么？" |
| Head 2 | Agent（执行头） | 执行 + 采集 | "按计划执行，收集证据" |
| Head 3 | Examiner（审查头） | 评估 + 学习 | "测试结果是否正确？" |

### 核心原则

1. **认知驱动测试**：先理解项目，再测试项目。`run` 命令的认知/测试比例根据已有知识自动调节
2. **AI 是决策者 + 执行者**：AI 自主探索、自主判断、自主修复（可选）
3. **确定性检查在数据库不变量领域优先于 AI 判断**：Checker 在 SQL/Go 函数检查范围内拥有裁决权；超出此范围的判断（API 响应语义、UI 行为）由 Judge 主导
4. **跨会话记忆**：积累项目知识和测试经验，越用越聪明
5. **最小配置启动**：第一次 `cerberus run` 只需要 LLM API key + 目标 URL + 测试目标描述
6. **完全自足**：所有外部系统（cccmemory、OpenWolf、codegraph、Playwright）均为**可选增强**。Cerberus 内置能力足以独立完成全部测试流程，无任何外部插件依赖

### 最低运行条件

```bash
# 只需三样，其他全部可选
1. 一个 LLM API key (ANTHROPIC_API_KEY / OPENAI_API_KEY / GEMINI_API_KEY)
2. 一个数据库（MVP 用 SQLite 文件，C4+ 可迁移至 PostgreSQL）
3. 一个目标 URL
```

### 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 技术栈 | 框架 Go，目标不限语言 | Go 实现框架，测试目标支持 Go/Python/Java/Node/Rust/PHP |
| 目标用户 | 有后端的 SaaS 开发者 | 需要测试 API + DB + UI + 跨服务链路 |
| 架构方案 | Skeleton + Cherry-pick | 新骨架 + 精选移植已验证模块 |
| 模式设计 | run + verify + serve | run 自动调节，verify 深度验证，serve CI 集成 |
| 记忆系统 | 混合型（统计+语义） | 结构化查询为主，embedding 为可选增强 |
| WebQA 关系 | 参考不集成 | 借鉴设计理念，代码独立实现 |
| 浏览器控制 | Playwright MCP 协议 | Go 无官方 Playwright 绑定，通过 MCP 通信复用已有基础设施 |
| AI 驱动模型 | 决策点调用 | AI 在认知/规划/执行/判断/恢复/学习 6 个决策点介入，非持续运行（见 §10） |
| 凭证管理 | 文件 + 环境变量 | 禁止 CLI 明文传密码（见 §2.4） |
| 成本控制 | Token Budget | 每 session 设上限，预算耗尽降级为纯 Checker（见 §14.3） |
| 外部依赖 | 零依赖启动 | 所有外部系统（cccmemory/codegraph/Playwright）为可选增强，内置能力覆盖全部核心流程 |

## 1. 三头核心架构

> **v3 架构变更**：从能力型划分（Explorer/Judge/Checker）改为生命周期型划分（Scout/Agent/Examiner）。
> 详见 `docs/2026-06-06-cerberus-heads-redesign.md`。

### 1.1 总体架构

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

### 1.2 接口定义

```go
// head/scout/scout.go — 侦察头：认知 + 规划
type Scout interface {
    Analyze(ctx context.Context, target TargetInfo) (*ProjectModel, error)
    Plan(ctx context.Context, goal string, model *ProjectModel) (*TestPlan, error)
}

// head/agent/agent.go — 执行头：执行 + 采集
type Agent interface {
    Execute(ctx context.Context, plan *TestPlan, actors []Actor) ([]Evidence, error)
}

// head/examiner/examiner.go — 审查头：评估 + 学习
type Examiner interface {
    Examine(ctx context.Context, evidence []Evidence, plan *TestPlan) ([]Verdict, error)
    Learn(ctx context.Context, verdicts []Verdict) error
}

// evidence/store.go — 线性证据收集
type EvidenceStore interface {
    Add(evidence Evidence)
    Query(filter EvidenceFilter) []Evidence
    All() []Evidence
}
```

### 1.3 裁决优先级（Examiner 内部）

> **适用范围**：Examiner 内部的确定性检查仅在 **数据库不变量 + HTTP 断言 + Diff 检查**
> 领域拥有裁决权。对于 API 响应语义、UI 行为等超出确定性检查能力的判断，
> 由 AI Judge 独立裁决，不存在矛盾场景。

当确定性检查和 AI Judge 都对同一目标给出判断时：

```
确定性: fail  + AI: pass  → 最终: fail（确定性优先）
确定性: pass  + AI: fail  → 最终: uncertain（矛盾）
确定性: pass  + AI: pass  → 最终: pass
确定性: fail  + AI: fail  → 最终: fail
```

当 Judge 和 Checker 都对同一目标给出判断时（即 Checker 注册了对应的检查函数）：

```
Checker: fail  + Judge: pass  → 最终: fail（确定性优先）
Checker: pass  + Judge: fail  → 最终: uncertain（矛盾）
Checker: pass  + Judge: pass  → 最终: pass
Checker: fail  + Judge: fail  → 最终: fail
```

### 1.4 Uncertain 处理流程（3 级降级链）

> **设计修正**：原设计仅说"升级人工"，但在 CLI 工具中"人工"无处安放。
> 现定义明确的 3 级降级链，确保 uncertain 有后续处理路径。

```
Uncertain 产生
  │
  ├─ Level 1: 自动重试（最多 2 次）
  │   ├─ 收集更多证据（重试操作 + 截图）
  │   ├─ 换用不同 prompt 重新 Judge
  │   └─ 如仍有分歧 → 进入 Level 2
  │
  ├─ Level 2: 降级为 Checker-only
  │   ├─ 放弃 AI 判断，仅保留确定性检查结果
  │   ├─ 如果该目标没有注册 Checker → 跳过 Level 2，直接进 Level 3
  │   ├─ 标记为 "checker_only: true"
  │   └─ 报告中展示证据 + Checker 结论
  │
  └─ Level 3: 标记待审
      ├─ 写入 .cerberus/pending-review.yaml
      ├─ 下次 `cerberus review` 命令交互式处理
      └─ 用户可标记为 pass/fail/ignore，更新 L3 程序记忆
```

**`cerberus review` 命令**（C5 实现）：

```bash
cerberus review
# 交互式展示每个 uncertain 裁决：
# [1/3] POST /api/v1/users — Judge: fail, Checker: pass
#   Evidence: { status: 201, body: {...} }
#   Judge reasoning: "Missing 'created_at' field"
#   Checker: "Row exists in users table"
#   Action: [p]ass / [f]ail / [s]kip / [d]etails ?
```

## 2. CLI 命令设计

### 2.1 `cerberus run` — 智能模式

自动调节认知/测试比例，认知-测试-判断-学习一体化：

```bash
# 最简用法（零配置）
cerberus run --url http://localhost:3000 --goal "测试所有管理后台 CRUD"

# 带角色凭证（引用 .cerberus/credentials.yaml 中的 name）
cerberus run --url http://localhost:3000 \
  --goal "测试用户注册到支付的完整流程" \
  --actor admin \
  --actor user

# 带数据库（启用 Checker 头）
cerberus run --url http://localhost:3000 \
  --goal "测试所有 API 端点" \
  --db "./myapp.db"
```

> **凭证安全**：`--actor` 只传角色名称，密码从 `.cerberus/credentials.yaml`（gitignore）或环境变量
> `CERBERUS_ACTOR_<NAME>_EMAIL` / `CERBERUS_ACTOR_<NAME>_PASSWORD` 读取。禁止在 CLI 明文传密码。

### 2.2 `cerberus verify` — 深度模式

基于积累的知识做精确验证 + 确定性检查全量运行。

与 `run` 的本质区别：
- `run`：认知 + 探索 + 适应性测试（可能发现新路径，更新 Project Model）
- `verify`：**只验证已知路径**，不探索新路径，不更新 Project Model（回归模式）

```bash
# 使用项目模型和 invariants
cerberus verify

# 指定配置
cerberus verify --config .cerberus/project.yaml
```

### 2.3 `cerberus serve` — 服务模式

长驻 HTTP API，支持 CI/CD 集成、Web UI、定时触发：

```bash
cerberus serve --port 8090
```

serve 模式 API 端点：

| Method | Path | 说明 |
|--------|------|------|
| POST | `/api/v1/sessions` | 触发测试（body: `{mode, goal, config?}`） |
| GET | `/api/v1/sessions/:id` | 查询 session 状态 |
| GET | `/api/v1/sessions/:id/report` | 获取 HTML/JSON 报告 |
| GET | `/api/v1/sessions` | 列出历史 sessions |
| GET | `/health` | 健康检查 |

### 2.4 `cerberus init` — 初始化项目

生成 `.cerberus/` 目录模板：

```bash
cerberus init                  # 交互式生成
cerberus init --url http://localhost:3000  # 带目标 URL
```

产出：
```
.cerberus/
├── project.yaml          # 项目配置模板
├── credentials.yaml      # 凭证模板（自动加入 .gitignore）
└── invariants/            # 用户自定义 invariant 文件
```

## 3. `run` 命令内部流程

### 3.1 Step 0: 项目识别

- 查找 `.cerberus/project.yaml`（有则加载）
- 查找 `.cerberus/project-model.yaml`（有则加载）
- 加载历史记忆
- 判断首次运行 vs 增量运行

### 3.2 Step 1: 认知（Scout 头）

**认知阶段遵循自足原则**：先尝试外部知识源（可选），没有则用内置能力完全覆盖。

```go
// 认知阶段伪代码
func (s *Scout) Analyze(ctx context.Context, target TargetInfo) (*ProjectModel, error) {
    // 1. 尝试外部知识源（可选加速，全部无依赖）
    var externalKnowledge []KnowledgeEntry
    for _, src := range e.knowledgeSources {
        if src.Available() {
            entries, _ := src.Query(ctx, e.goal)
            externalKnowledge = append(externalKnowledge, entries...)
        }
    }

    // 2. 内置探索（永远执行，保底，语言无关）
    surfaceData := e.recon.Crawl(ctx, e.url)      // HTTP client，零依赖
    codeData := e.codeProvider.Analyze(ctx)         // patternscan + manifest + openapi
    var dataData *SchemaInsight
    if e.db != nil {
        dataData = e.analyzeSchema(ctx)              // information_schema 查询
    }

    // 3. 合并：外部知识 + 自行探索 → Project Model（每条知识标注 confidence）
    return e.mergeToModel(externalKnowledge, surfaceData, codeData, dataData)
    // 确定性来源 (patternscan/openapi) → confidence 0.9+
    // AI 推理来源 → confidence 0.5-0.7
    // 测试阶段按 confidence 升序验证 → 自校正闭环
}
```

三维度并行分析（内置能力，不依赖任何插件，不依赖任何特定语言）：

| 维度 | 内置能力 | 产出 | 外部增强（可选） |
|------|----------|------|------------------|
| Surface | HTTP client 爬取页面 | 页面列表、导航流 | Playwright MCP → JS 渲染 + 表单/交互识别 |
| Code | patternscan（多语言正则）+ manifest + openapi | API 端点、技术栈、依赖 | 语言插件 / codegraph MCP → 精确调用链 |
| Data | information_schema 查询 | 表关系、约束 | — |

**增量运行：差异认知**

- 对比上次项目模型，识别变更
- 只分析变更部分
- 如有外部知识源，对比外部知识是否过期

产出：Project Model → 存入记忆。

### 3.2.1 三层弥补模型与置信度自校正

Cerberus 的认知不追求一次完美，而是通过**三层弥补 + 自校正闭环**逐步逼近完整：

```
第 1 层：内置通用分析（永远可用）
  patternscan + manifest + openapi + DB schema + Surface 爬取
  → 匹配到的部分初始置信度 0.9+
  → 覆盖度因项目而异：Express 应用可能 90%，Spring Boot DI 可能 20%
  → 由测试阶段校准（不预设固定百分比）

第 2 层：AI 认知推理（核心差异化）
  AI 从第 1 层的线索推理出完整项目模型
  → AI 推理部分的初始置信度 0.5-0.7
  → 例："发现 /admin/users 页面 + users 表 → 推理存在 CRUD API → 推理有权限检查"
  → 由测试阶段校准（推理正确则 confidence 升高，否则删除）

第 3 层：外部插件（可选增强）
  codegraph / gostatic / pythonstatic / cccmemory / OpenWolf
  → 精确分析工具产出初始置信度 0.95+
  → 覆盖度取决于插件质量和项目匹配度
```

**覆盖度校准机制**：
- 每次测试后统计"预测正确"和"预测错误"的比例
- 连续 3 次 run 后，用实际比例修正每层的初始置信度
- 例：若 patternscan 在某项目中 80% 的端点预测正确，则该项目下第 1 层置信度修正为 0.8

**置信度自校正闭环**：Project Model 中每条知识标注 confidence，测试阶段自动校准：

```yaml
# project-model.yaml（含置信度标注）
api:
  endpoints:
    - method: POST
      path: /api/v1/users
      existence_confidence: 0.95          # 端点确实存在（来自 patternscan）
      correctness_confidence: 0.0         # 功能正确性未验证
    - method: POST
      path: /api/v1/refund
      existence_confidence: 0.6           # 端点可能存在（AI 推理）
      correctness_confidence: 0.0         # 功能正确性未验证

invariants_hints:
  - id: auto-001
    description: "wallet_balance 不能为负"
    existence_confidence: 0.8             # 这条规则确实存在
    correctness_confidence: 0.0           # 是否被正确执行未验证
    source: code_analysis
  - id: infer-001
    description: "退款金额不能超过原订单金额"
    existence_confidence: 0.5             # 规则可能存在（AI 推理）
    correctness_confidence: 0.0           # 是否被正确执行未验证
```

> **语义修正**：原设计用单一 `confidence` 同时表示"存在性"和"正确性"，导致混淆。
> "端点存在"和"端点功能正确"是两个独立命题，需要分开跟踪。
> - `existence_confidence`：端点/规则**确实存在**的概率
> - `correctness_confidence`：端点/规则**功能正确**的概率
> 
> 首次认知时 `correctness_confidence = 0`（未测试），测试后才更新。

**测试阶段的自校正流程**：

```
1. 生成测试计划时，按 existence_confidence 升序排序 → 低存在性的推理优先验证

2. 验证存在性：
   推理 "存在 POST /api/v1/refund" (existence: 0.6)
     → 发送请求，返回 200/201/400/401/403 → existence_confidence 升到 0.95 ✅
     → 发送请求，返回 404 → 从模型中删除 ❌

3. 验证正确性（存在性确认后）：
   不变量 "退款不超过原订单" (correctness: 0.0)
     → 测试超限退款被拒绝 → correctness_confidence 升到 0.9 ✅
     → 测试超限退款成功 → 标记为 bug，correctness_confidence 保持 0 ⚠️

4. 校正结果写回 Project Model 和 L2 语义记忆
   → 下一次 run 使用校正后的模型
   → existence 越用越准，correctness 随测试覆盖率提升
```

这意味着**首次 run 不怕不准**（AI 推理大胆猜测），**后续 run 自动纠错**（测试验证闭环）。

认知/测试比例根据 **已知信息量评分** 自动调节：

> **逻辑修正**：原 maturity 公式使用 `count / total` 比率，但 `total`（总页面数、总端点数）
> 本身是未知的——这正是认知阶段要发现的。改为基于**已知信息的绝对量和质量**，
> 使用软饱和（soft saturation）归一化，不需要知道"总共有多少"。

```
known_info_score = weighted_sum(
    min(known_endpoints, 20) / 20 × avg_confidence,   # 40% 权重：端点（20个即满）
    min(known_pages, 30) / 30 × avg_confidence,       # 30% 权重：页面（30个即满）
    schema_analyzed ? 1.0 : 0.0,                       # 20% 权重：DB schema 是否已分析
    has_historical_model ? 1.0 : 0.0,                  # 10% 权重：是否有历史项目模型
)
# 结果范围 0-1，无需除以 max_possible_score（软饱和天然归一化）

known_info_score < 0.3  →  learn 80%, test 20%   # 认知不足：多做探索
known_info_score 0.3-0.7 → learn 30%, test 70%   # 认知足够：开始测试
known_info_score > 0.7  →  learn 5%,  test 95%   # 认知充分：专注测试
```

**认知终止条件**（不再用"比例"，而是用"信息增量"）：

```
每次认知迭代后计算：
  delta = new_info_score - previous_info_score
  if delta < 0.05 for 2 consecutive iterations:
      # 认知收敛，停止探索，进入测试
      break
```

### 3.3 Step 2: 规划（Scout 头）

- 根据目标 + 项目模型 + 记忆策略生成测试计划
- **置信度驱动排序**：低 confidence 的推理优先验证（自校正闭环）
- 记忆驱动优先级：高风险的先测、常挂的先测
- 覆盖度门控：确保不遗漏关键路径
- 测试结果用于校正 Project Model（见 §3.2.1）

### 3.4 Step 3: 执行 + 采集（Agent 头）

- 按计划逐步执行操作
- 每步采集证据：截图 + API 响应 + DB snapshot
- 失败自适应恢复（重试 / 换路径 / 跳过）
- 有 DB 时自动做 before/after diff

### 3.5 Step 4: 评估（Examiner 头）

- AI Judge: 评估每条证据 → Verdict
- 确定性检查: 运行 SQL/HTTP/Diff 检查（如果有定义）
- 裁决合并（确定性 > AI）

- Judge: AI 评估每条证据 → Verdict
- Checker: 运行确定性检查函数（如果有）
- 裁决合并（Checker > Judge）

### 3.6 Step 5: 学习 + 报告

- 写入 L1 情景记忆（测试事件）
- 提炼写入 L2 语义记忆（新发现的知识）
- 更新 L3 程序记忆（策略有效性）
- 生成 HTML + JSON 报告
- 更新 Project Model

## 4. Scout 头详解

> **v3 变更**：原 Explorer 头的代码分析 + 认知推理 + 测试规划留在 Scout。
> 浏览器操作 + 执行 + 失败恢复移到 Agent 头（见 §4b）。

### 4.1 CodeProvider 插件接口

```go
// head/explorer/code_provider.go
type CodeProvider interface {
    Name() string
    Analyze(ctx context.Context, projectRoot string) (*CodeInsight, error)
    Capabilities() []string
}

type CodeInsight struct {
    Routes        []RouteDef
    CallChains    []CallChain
    BusinessRules []BusinessRule
    DataFlows     []DataFlow
    Invariants    []InvariantHint
}
```

**语言无关的分层策略**：

> Cerberus 用 Go 实现，但**测试目标不限语言**。Go/Python/Java/Node/Rust 项目均可测试。
> 语言特定的深度分析全部通过插件提供，内置能力不依赖任何特定语言。

| 层级 | Provider | 说明 | 语言依赖 | 优先级 |
|------|----------|------|----------|--------|
| **内置** | `patternscan` | 多语言路由正则扫描（Go/Flask/Spring/Express/Axum） | 无（纯正则） | 默认启用 |
| **内置** | `openapi` | OpenAPI/Swagger/GraphQL Schema 解析 | 无 | 检测到文件时启用 |
| **内置** | `manifest` | 依赖清单解析（package.json/go.mod/requirements.txt/pom.xml） | 无 | 检测到文件时启用 |
| **内置** | `config` | 配置文件扫描（docker-compose/nginx/.env） | 无 | 检测到文件时启用 |
| **插件** | `gostatic` | Go AST 深度分析（go/packages） | Go | 可选 |
| **插件** | `pythonstatic` | Python AST 分析（无 import） | Python | 可选（后续） |
| **插件** | `jsstatic` | TypeScript/JS 分析 | JS/TS | 可选（后续） |
| **插件** | `javastatic` | Java 字节码/源码分析 | Java | 可选（后续） |
| **插件** | `codegraph` | 对接 codegraph MCP（精确调用链） | 任意（需 MCP server） | 可选 |

**内置 patternscan 示例**（覆盖 6+ 语言框架的常见路由注册模式）：

```go
// head/explorer/code/patternscan.go
var routePatterns = []RoutePattern{
    // Go (gin/chi/echo/fiber)
    {Lang: "go", Pattern: `\.(GET|POST|PUT|DELETE|PATCH)\("([^"]+)"`},
    // Python Flask
    {Lang: "python", Pattern: `@app\.route\("([^"]+)"\s*(?:,\s*methods=\[([^\]]+)\])?`},
    // Python FastAPI
    {Lang: "python", Pattern: `@(app|router)\.(get|post|put|delete)\("([^"]+)"`},
    // Java Spring
    {Lang: "java", Pattern: `@(Get|Post|Put|Delete|Patch)Mapping\("([^"]+)"\)`},
    // Node Express/Fastify
    {Lang: "javascript", Pattern: `(app|router)\.(get|post|put|delete)\(['"]([^'"]+)['"]`},
    // Rust Axum/Actix
    {Lang: "rust", Pattern: `\.(get|post|put|delete)\("([^"]+)"\)`},
}
```

> 覆盖度约 50-60%（精确度不如 AST 分析），配合 AI 认知推理可提升至 85%+。
> 语言特定插件（gostatic 等）提供精确分析，将覆盖度提升至 95%。

### 4.1.2 外部知识源接口

Cerberus 认知阶段可读取已安装的外部知识系统，**加速认知但非必须**：

```go
// head/explorer/knowledge_source.go
type KnowledgeSource interface {
    Name() string
    Available() bool  // 检测是否安装/配置（自动发现）
    Query(ctx context.Context, topic string) ([]KnowledgeEntry, error)
}

type KnowledgeEntry struct {
    Source   string  // "cccmemory" | "openwolf" | "codegraph" | ...
    Content  string
    Tags     []string
    Confidence float64
}
```

内置适配器（全部可选）：

| 适配器 | 数据来源 | 发现条件 | 加速效果 |
|--------|----------|----------|----------|
| `cccmemory` | cccmemory MCP server | MCP 配置中存在 cccmemory | 项目决策、架构知识 |
| `openwolf` | `.wolf/cerebrum.md` + `.wolf/memory.md` | 项目根目录存在 `.wolf/` | 用户偏好、已知坑、学习记录 |
| `codegraph` | codegraph MCP server | MCP 配置中存在 codegraph | 完整代码图谱（调用链、路由、依赖） |

降级策略：`Available()` 返回 false 时直接跳过，零开销。即使全部不可用，
Cerberus 的 Surface 爬取 + patternscan + DB schema 分析仍能完成完整认知。

### 4.1.1 浏览器控制：Playwright MCP 方案

Go 没有官方 Playwright 绑定（仅 Node/Python/.NET/Java）。Cerberus 通过 **MCP 协议**
与 Playwright MCP Server 通信，复用项目已有的 MCP 客户端基础设施。

```
Explorer (Go)
  → MCP Client (已有: github.com/mark3labs/mcp-go)
    → Playwright MCP Server (Node.js, @anthropic/mcp-playwright)
      → Chromium Browser
```

优势：
- 无需 Go 绑定，依赖成熟的 Node.js Playwright
- 与现有 ast-graph/codegraph MCP 集成架构一致
- 可复用 Playwright MCP 的 snapshot、click、type 等工具

备选方案（仅在 MCP 方案不满足性能需求时评估）：
- `playwright-go`（社区库，成熟度待验证）
- 嵌入 Node.js runtime（goja + Playwright）

### 4.2 项目模型产出

`run` 认知阶段产出 `.cerberus/project-model.yaml`：

```yaml
project:
  name: "my-saas"
  tech_stack: [go, react, postgres]   # 由 manifest 分析确定
  tech_stack_confidence: 0.95          # 来自确定性来源

navigation:
  pages:
    - path: /login
      type: auth
      confidence: 0.95                 # Surface 爬取确认
    - path: /admin/users
      type: crud
      requires_auth: true
      requires_role: admin
      confidence: 0.9
    - path: /admin/billing
      type: crud
      confidence: 0.5                  # AI 推理（从导航结构推测）

api:
  endpoints:
    - method: POST
      path: /api/v1/users
      confidence: 0.95                 # patternscan 确认
    - method: POST
      path: /api/v1/refund
      confidence: 0.6                  # AI 推理（从 billing 页面推测）

invariants_hints:
  - id: auto-001
    source: code_analysis
    description: "wallet_balance 不能为负"
    confidence: 0.9
    severity: critical
  - id: infer-001
    source: ai_inference
    description: "退款金额不能超过原订单金额"
    confidence: 0.5                    # AI 从业务语义推理
    severity: high
```

> **设计修正**：原设计包含 `total_pages` / `total_endpoints` 字段用于计算覆盖率，
> 但这些值在认知阶段是未知的——"总共有多少"正是认知要发现的。
> 现在改用 `InfoScore()` 方法，基于已知信息的绝对量（带软饱和）计算分数，
> 不依赖任何 "total" 字段。

每条知识的 `confidence` 决定其在测试阶段的行为（见 §3.2.1 自校正闭环）：
- **confidence ≥ 0.9**：确定性来源，直接作为测试依据
- **confidence 0.5-0.9**：需验证，优先安排测试
- **confidence < 0.5**：弱推理，仅作参考，不自动生成测试

此模型是 verify 模式的输入，也是跨会话记忆的一部分。每次 run 后根据测试结果校正 confidence。

## 5. Agent 头详解

> **v3 新增**：从原 Explorer 拆出执行职责，成为独立头。
> 职责：按 TestPlan 执行测试步骤，采集证据。不做任何判断。

### 5.1 执行策略

规则引擎优先处理确定性操作（~70%），AI 仅在页面异常、路径分支等场景介入。
详见 `docs/2026-06-06-cerberus-heads-redesign.md` §3。

### 5.2 浏览器控制：Playwright MCP

Go 没有官方 Playwright 绑定。Cerberus 通过 MCP 协议与 Playwright MCP Server 通信。
详见 `docs/2026-06-06-cerberus-design.md` §4.1.1（保留原设计）。

### 5.1 Verdict 数据模型

```go
type Verdict struct {
    ID                     string
    Target                 string
    Status                 VerdictStatus  // pass / fail / uncertain / skip
    ExistenceConfidence    float64        // 0.0 - 1.0: 端点/规则确实存在的概率
    CorrectnessConfidence  float64        // 0.0 - 1.0: 功能正确的概率
    Reasoning              string
    Evidence               []string
    Suggestions            []string       // fail 时的修复建议
    Metadata               map[string]any
}
```

> **语义修正**：原设计单一 `Confidence` 同时表达"存在性"和"正确性"，导致混淆。
> 现拆分为两个独立字段，分别跟踪。

### 5.2 置信度策略

```
fail + correctness_conf ≥ 0.7  → 确认失败，进入修复流程
fail + correctness_conf < 0.7  → uncertain → 进入 §1.4 降级链
pass + correctness_conf ≥ 0.9  → 确认通过
pass + correctness_conf < 0.9  → 通过，标记 "建议抽查"
uncertain                      → 进入 §1.4 降级链（3 级处理）
```

### 5.3 Judge 执行流程

1. 构造判断 prompt（结构化证据 + 期望）
2. 检查 Token Budget 是否充足（不足则降级为 skip）
3. 调 LLM（带 vision 如果有截图）
4. 解析结构化输出为 Verdict
5. 不确定时追加一次深度判断（最多 2 次重试）
6. 策略引擎应用置信度规则
7. 记录本次 LLM 调用 token 消耗到 budget

## 6. Examiner 头详解

> **v3 变更**：合并原 Judge 头 + Checker 头 + Arbitrator 为 Examiner。
> AI 判断和确定性检查作为内部子模块，通过类型路由调度。

### 6.1 Verdict 数据模型

```go
type Verdict struct {
    ID                     string
    Target                 string
    Status                 VerdictStatus  // pass / fail / uncertain / skip
    ExistenceConfidence    float64        // 0.0 - 1.0: 端点/规则确实存在的概率
    CorrectnessConfidence  float64        // 0.0 - 1.0: 功能正确的概率
    Reasoning              string
    Evidence               []string
    Suggestions            []string       // fail 时的修复建议
    Source                 string         // "judge" | "checker" | "merged"
    Metadata               map[string]any
}
```

### 6.2 评估路由

每条证据自动匹配检查类型：
- 有 SQL/HTTP/Diff 断言定义 → 确定性检查（零 AI）
- 其他 → AI Judge
- 同一目标有多种结果 → 裁决合并（确定性 > AI）

### 6.3 确定性检查（子模块）

多域确定性检查引擎。覆盖 Judge 和纯 SQL 之间的"半确定性"地带——
不需要 AI 但超出简单 SQL 查询的断言场景。

### 6.1 架构：三层检查能力

```
Checker（检查头）
├── Core（内置，零依赖）
│   ├── SQL Checker     — SQL + assertion 断言
│   ├── HTTP Checker    — 状态码 / Header / Body contains / JSON path 取值
│   └── Diff Checker    — DB before/after snapshot diff
│
├── Enhanced（可选，引入开源库）
│   ├── JSON Schema     — gojsonschema（响应体验证）
│   ├── OpenAPI Contract — kin-openapi（API 契约验证）
│   └── Performance     — 响应时间 / 吞吐量断言
│
└── Plugin（用户自定义）
    └── Go 函数注册
```

### 6.2 Core 检查类型

**SQL 检查**（移植自 relay-test-harness，去 Relay 绑定）：

```yaml
invariants:
  - id: INV-001
    description: "用户余额不能为负"
    check: "SELECT COUNT(*) AS cnt FROM users WHERE balance < 0"
    assertion: "cnt == 0"
```

**断言语法**：`assertion` 字段支持：
- `cnt == 0` / `cnt > 0` / `cnt <= N`：标量比较
- `empty`：结果集为空 → pass
- `not_empty`：结果集非空 → pass
- 省略 `assertion` 时默认：查询返回 0 行 → pass

**HTTP 检查**（新增，零依赖）：

```yaml
checks:
  - id: CHK-001
    type: http
    description: "POST /api/v1/users 返回 201"
    target: "POST /api/v1/users"
    assertions:
      - status == 201
      - header "Content-Type" contains "json"
      - body.id != null
      - body.email contains "@"
      - body_json_path("$.created_at") != null

  - id: CHK-002
    type: http
    description: "列表接口分页正常"
    target: "GET /api/v1/users?page=1&limit=10"
    assertions:
      - status == 200
      - body_json_path("$.items") is_array
      - body_json_path("$.items.length") <= 10
```

HTTP 断言语法：
- `status == N` / `status >= N` / `status != N`
- `header "Name" contains "value"`
- `body.field != null` / `body.field contains "text"`
- `body_json_path("$.path")` 支持简单 JSON path 取值后比较
- `is_array` / `is_number` / `is_string` 类型检查

**Diff 检查**（新增，零依赖，需 DB 连接）：

```yaml
checks:
  - id: CHK-010
    type: diff
    description: "创建用户后 users 表新增一行"
    target: "POST /api/v1/users"
    table: users
    operation: insert         # insert / update / delete
    expected_delta: +1        # 预期行数变化
```

Diff 检查自动在操作前后做 DB snapshot，比较目标表的行数变化。
支持精确匹配（`expected_delta: +1`）和范围匹配（`expected_delta: >=1`）。

### 6.3 Enhanced 检查类型（可选依赖）

当检测到对应 Go 模块时自动启用：

| 检查类型 | 依赖库 | 触发条件 |
|----------|--------|----------|
| JSON Schema | `github.com/xeipuuv/gojsonschema` | go.mod 中存在 |
| OpenAPI Contract | `github.com/getkin/kin-openapi` | 项目中存在 OpenAPI spec 文件 |
| Performance | 无（内置 timer） | 用户配置 `max_response_time` |

**JSON Schema 检查**：

```yaml
checks:
  - id: CHK-020
    type: json_schema
    description: "用户响应符合 User schema"
    schema: "./schemas/user.json"
    target: "GET /api/v1/users/:id"
```

**OpenAPI Contract 检查**：

```yaml
checks:
  - id: CHK-021
    type: openapi_contract
    description: "所有 API 符合 OpenAPI spec"
    spec: "./openapi.yaml"
    # 自动验证每个端点的请求/响应是否符合 spec
```

### 6.4 Plugin 检查（用户自定义）

```go
checker.Register("CHK-100", func(ctx context.Context, ev Evidence) CheckResult {
    // 自定义 Go 检查逻辑
    if ev.StatusCode != 201 {
        return CheckResult{Status: "fail", Reason: "expected 201"}
    }
    return CheckResult{Status: "pass"}
})
```

### 6.5 检查能力对比

| 检查类型 | 需 AI | 需 DB | 确定性 | 覆盖场景 |
|----------|-------|-------|--------|----------|
| SQL | ✗ | ✓ | 100% | 数据不变量 |
| HTTP | ✗ | ✗ | 100% | API 响应格式/状态码 |
| Diff | ✗ | ✓ | 100% | 操作副作用验证 |
| JSON Schema | ✗ | ✗ | 100% | 响应体结构验证 |
| OpenAPI | ✗ | ✗ | 100% | API 契约合规 |
| Go Plugin | ✗ | ✗ | 100% | 任意自定义逻辑 |
| **Judge** | ✓ | ✗ | ~85% | 语义正确性、UI 行为、复杂业务逻辑 |

Checker 覆盖"格式正确性"，Judge 覆盖"语义正确性"。
两者互补而非竞争——Checker 不浪费 token 做确定性检查，Judge 不在结构验证上浪费能力。

## 7. 混合记忆系统

### 7.0 设计原则：自足 + 可选增强

Cerberus 记忆系统**完全自建**，不依赖任何外部记忆系统。理由：

1. **通用框架**：用户不一定安装了 cccmemory / OpenWolf
2. **领域特定**：测试事件、覆盖度、策略有效性是测试框架独有概念
3. **闭环驱动**：记忆直接改变测试行为（优先级、顺序、策略），需要严格的 schema

外部记忆系统（cccmemory、OpenWolf）作为 **认知阶段的可选加速源**（见 §4.1.2 KnowledgeSource），
而非记忆存储的后端。关系：

```
外部记忆系统（cccmemory/OpenWolf）
  → 认知阶段读取（可选，加速首次 run）
  → 产出喂入 Cerberus L2 语义记忆（Cerberus 自己的库）

Cerberus 记忆系统
  → L1/L2/L3 全部存储在 Cerberus 自己的 PG 中
  → 不向外部系统写入（职责边界清晰）
```

### 7.1 三层架构

```
L1: Episodic（情景记忆）— Cerberus 独有，必须自建
  "上次测试 admin/users 页面发现了一个 500 错误"
  每次 run 自动记录，30 天自动衰减
  存储: SQLite (MVP) → PostgreSQL (C4+)
  检索: 结构化查询（按目标/时间/状态）
  没有外部系统能提供这种数据

L2: Semantic（语义记忆）— Cerberus 自建，外部可加速
  "这个项目的用户认证用 JWT + HMAC-SHA256"
  主要来源：认知阶段提炼（CodeProvider + Surface 爬取）
  可选加速：首次 run 时从 KnowledgeSource 读取已有项目知识
  存储: SQLite (MVP) → PostgreSQL + pgvector (C4+)
  检索: 三层降级（结构化 → 全文 → embedding）

L3: Procedural（程序记忆）— Cerberus 独有，必须自建
  "测试 CRUD 操作时，先测创建再测删除效果最好"
  从多次测试中提炼的策略和规则
  存储: SQLite (MVP) → PostgreSQL (C4+)
  检索: 按场景匹配
  没有外部系统能提供测试策略知识
```

### 7.2 检索降级链路

```
查询发起
  → 结构化查询 (SQL/tags，永远可用)
  → 全文搜索 (tsvector，几乎零成本)
  → 语义搜索 (embedding，可选启用)
```

### 7.3 Embedding 定位

Embedding 是**可选增强层**，不作为核心依赖：

| 方案 | 优先级 | 依赖 | 向量维度 |
|------|--------|------|----------|
| Ollama 本地 (nomic-embed-text) | 主力（自动检测） | ollama | 768 |
| OpenAI embedding API (text-embedding-3-small) | 备选 | OPENAI_API_KEY | 1536 |
| PostgreSQL tsvector | 兜底（永远可用） | 无 | — |

> **维度兼容**：不同 provider 产出不同维度的向量。数据库 `embedding` 列的维度在
> **首次初始化时根据选定 provider 固定**（通过配置项 `embedding.dimension`）。
> 切换 provider 需要重建 embedding 列（提供 `cerberus migrate-embedding` 命令）。

### 7.4 代码知识图谱与记忆的关系

**代码知识图谱**（ast-graph/codegraph）是实时结构数据源，**记忆系统**是经验累积层。两者互补：

| 维度 | 代码知识图谱 | 记忆系统 |
|------|-------------|---------|
| 性质 | "代码现在长什么样" | "我们学到了什么经验" |
| 生命周期 | 随代码刷新 | 跨会话持久 |
| 角色 | 认知阶段的数据源 | 测试阶段的上下文 |
| 降级 | 不可用时退到记忆 | 图谱挂了记忆兜底 |

交互模式：
1. 认知阶段：CodeProvider 查图谱 → 提炼知识 → 存入语义记忆
2. 测试执行：遇到异常 → 查图谱获取上下文 → Judge 判断
3. 会话结束：查询结果提炼 → 持久化到记忆（图谱刷新了记忆还在）

## 8. 项目插件系统

### 8.1 项目描述 Schema

`.cerberus/project.yaml` — 用户手写或 `run` 认知阶段自动生成：

```yaml
project:
  name: "my-saas"

services:
  - name: web
    url: "http://localhost:3000"
    health: "/"
  - name: api
    url: "http://localhost:8080"
    health: "/health"

actors:
  - name: admin
    credentials: { email: "${ADMIN_EMAIL}", password: "${ADMIN_PASS}" }
    entry: "/admin"
  - name: user
    credentials: { email: "${USER_EMAIL}", password: "${USER_PASS}" }
    entry: "/dashboard"

databases:
  - name: main
    url: "${DATABASE_URL}"

code:
  root: "./src"
  providers: [gostatic]

invariants:
  - id: INV-001
    description: "用户余额不能为负"
    severity: critical
    check: "SELECT COUNT(*) FROM users WHERE balance < 0"

settings:
  max_duration: 30m
  confidence_threshold: 0.7
  auto_fix: low_only
```

所有字段均为可选。零配置时只有 `--url` 和 `--goal` 即可运行。

## 9. LLM 客户端

```go
// llm/client.go
type Client interface {
    Complete(ctx context.Context, req Request) (*Response, error)
    CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error)
}

// llm/claude.go   — Anthropic Claude
// llm/openai.go   — OpenAI GPT
// llm/gemini.go   — Google Gemini
```

自动探测：根据 model 名称前缀选择 provider（`claude-*` → Anthropic，`gpt-*` → OpenAI，`gemini-*` → Gemini）。

## 10. AI 驱动模型

### 10.1 核心理念

Cerberus **不在运行时持续调用 AI**。AI 在 6 个明确的**决策点**介入，其余由确定性逻辑驱动。

```
确定性调度器（Go）负责：
  循环、超时、重试、并发、数据采集、报告生成

AI（LLM）负责：
  理解、规划、判断、恢复、学习 —— 需要"思考"的环节
```

这样做的理由：
- **成本可控**：每个决策点有独立的 token budget
- **可观测**：每次 AI 调用都有明确的输入/输出，便于调试
- **可降级**：某个决策点 AI 失败，不影响其他环节

### 10.2 六个决策点

| 决策点 | 何时调用 | 输入 | 期望输出 | 调用频率 |
|--------|----------|------|----------|----------|
| **认知** `Analyze` | 首次/增量运行开始 | CodeProvider 数据 + 表面爬取 | Project Model YAML | 1-3 次/session |
| **规划** `Plan` | 认知完成后 | Project Model + Goal + 记忆 | TestPlan（步骤列表） | 1-2 次/session |
| **执行引导** `Steer` | 规则无法处理时 | 当前页面 snapshot + 剩余计划 | 下一步 Action | 按需（见 §10.2.1） |
| **恢复** `Recover` | 操作失败时 | 失败上下文 + 历史操作 | 新路径 / 跳过 / 放弃 | 按需，最多 3 次/步骤 |
| **判断** `Judge` | 每条证据 | 截图 + API 响应 + 期望 | Verdict（pass/fail/uncertain） | 每证据 1-2 次 |
| **学习** `Learn` | Session 结束 | 全部 Verdict + 已有记忆 | 新/更新记忆条目 | 1 次/session |

### 10.2.1 执行引导 `Steer` 决策树

> **逻辑修正**：原设计"每步操作都调 AI"，但大多数 UI 操作是确定性的
> （"点击登录按钮"、"输入 admin"），不需要 AI。每步调 AI 导致：
> - 成本翻倍（20 步 × 2K tokens = 40K）
> - 延迟增加（每步 2-5 秒）
> - AI 在简单操作上可能引入幻觉错误

```
当前步骤 + 页面状态
  │
  ├─ 规则引擎优先处理（0 token, 0 延迟）
  │   ├─ 步骤明确指定了 selector + action → 直接执行
  │   ├─ 页面匹配预期（URL/path 匹配计划）→ 执行预设操作
  │   └─ 表单字段有 placeholder/label 匹配步骤描述 → 自动填充
  │
  ├─ 需要 AI 介入的场景
  │   ├─ 页面结构与预期不符（新弹窗、新页面、结构变化）
  │   ├─ 操作失败且非网络错误（权限不足、验证错误）
  │   ├─ 需要决定测试路径分支（A/B 测试、多角色场景）
  │   └─ 步骤描述模糊（"测试用户注册" 但无具体步骤）
  │
  └─ 统计
      规则处理：~70% 的操作（确定性，快速）
      AI 介入：~30% 的操作（复杂决策）
      → 实际 Steer 调用：20 步中约 6 次，而非 20 次
```

**预期成本修正**：

| 决策点 | 调用次数（修正前） | 调用次数（修正后） | tokens |
|--------|-------------------|-------------------|--------|
| 执行引导 | 20 | **~6** | 12K（原 40K） |

### 10.3 AI Driver 接口

AI Driver 是三头共享的 AI 调用层，统一管理 prompt 构造、token 预算、结果解析：

```go
// ai/driver.go
type Driver interface {
    // 核心方法：构造 prompt → 调 LLM → 解析为结构化输出
    Decide(ctx context.Context, prompt Prompt, schema interface{}) error

    // 带 vision 的决策（截图判断）
    DecideWithVision(ctx context.Context, prompt Prompt, images []Image, schema interface{}) error

    // Token 预算管理
    Budget() *TokenBudget
}

type Prompt struct {
    System   string          // 角色定义（Explorer/Judge/Learner）
    Context  []ContextEntry  // 记忆检索 + CodeProvider 数据 + 历史操作
    Task     string          // 当前决策点的具体任务
    Output   string          // 期望的输出格式描述（自然语言）
}

type ContextEntry struct {
    Source string  // "memory" | "codegraph" | "history" | "snapshot"
    Content string
    Relevance float64  // 检索相关性分数
}

type TokenBudget struct {
    SessionTotal int   // session 总 token 上限（默认 200K）
    Spent        int
    PerCallLimit int   // 单次调用上限（默认 10K）
    // Remaining() = SessionTotal - Spent
}
```

### 10.4 Prompt 构造流水线

每个决策点都经过统一的 prompt 构造流程：

```
1. 角色模板（System Prompt）
   └─ Explorer 角色有浏览器操作指引
   └─ Judge 角色有判断标准定义
   └─ Learner 角色有知识提炼指引

2. 上下文注入
   ├─ 记忆检索结果（AI Driver 从 Memory System 查询）
   ├─ CodeProvider 数据（如果是认知/规划决策点）
   └─ 历史操作流（如果是执行/恢复决策点）

3. 任务描述
   └─ 决策点特定的任务指令

4. 输出格式
   └─ JSON Schema 描述（强制 LLM 输出结构化数据）

→ LLM 调用（带 token budget 检查）
→ 解析 + Schema 校验
→ 失败时重试（最多 1 次，收紧 prompt）
```

### 10.5 AI 降级策略

| 场景 | 降级方案 | 用户体验 |
|------|----------|----------|
| LLM 完全不可用 | 纯 Checker 模式（只运行确定性检查） | 报告标注 "AI unavailable, checker-only" |
| LLM 超时（>30s） | 重试 1 次 → 跳过当前步骤 | 对应 verdict 标记 `skip` |
| Token budget 耗尽 | 停止所有 AI 调用，用已有结果 | 报告标注 "budget exhausted at step N" |
| 响应解析失败 | 重试 1 次（更严格的 prompt + schema） | 2 次均失败 → 标记 `uncertain` |
| 截图过大（>5MB） | 压缩到 1MB → 裁剪敏感区域 | 裁剪失败 → 降级为纯文本判断 |
| 认知阶段 LLM 失败 | 使用上次 Project Model（如有） | 标注 "stale model, re-cognition recommended" |

### 10.6 AI 调用频率

| 决策点 | 归属头 | 调用频率 |
|--------|--------|----------|
| 认知 | Scout | 每 session 1-3 次（增量运行时 1 次） |
| 规划 | Scout | 每 session 1-2 次 |
| 执行引导 | Agent | 按需（~30% 的操作需要 AI 介入） |
| 恢复 | Agent | 按需（失败时触发，每步骤最多 3 次） |
| 判断 | Examiner | 每条证据 1-2 次 |
| 学习 | Examiner | 每 session 1 次 |

Token 消耗因项目规模和复杂度而异，不做固定估算。默认 session budget = 200K tokens。

## 11. 包结构

```
projects/cerberus/
├── cmd/cerberus/main.go              # CLI: init / run / verify / serve
├── internal/
│   ├── ai/                           # AI 驱动层（§10）
│   │   ├── driver.go                 # AIDriver 接口 + TokenBudget
│   │   ├── prompt.go                 # Prompt 构造流水线
│   │   ├── context.go                # 上下文注入（记忆/代码/历史）
│   │   ├── parser.go                 # 结构化输出解析 + Schema 校验
│   │   └── budget.go                 # Token 预算管理
│   ├── head/
│   │   ├── scout/                     # Head 1: 侦察头 — 认知 + 规划
│   │   │   ├── scout.go               # Scout 接口 + 协调三维度扫描
│   │   │   ├── recon.go               # 并行三维度扫描（Surface + Code + Data）
│   │   │   ├── analyzer.go            # AI 认知推理（合并线索 → ProjectModel）
│   │   │   ├── planner.go             # 测试规划（元认知：优先级、排序、覆盖度门控）
│   │   │   ├── knowledge.go           # 外部知识源接口（可选增强）
│   │   │   └── code/                  # 内置代码分析器（语言无关）
│   │   │       ├── patternscan.go     # 多语言路由正则扫描
│   │   │       ├── manifest.go        # 依赖清单解析 (go.mod/package.json/requirements.txt)
│   │   │       ├── openapi.go         # OpenAPI/Swagger/GraphQL 解析
│   │   │       ├── configscan.go      # docker-compose/nginx 等配置扫描
│   │   │       └── lang/              # 语言特定插件（可选）
│   │   │           ├── gostatic.go    # Go AST（可选插件）
│   │   │           └── codegraph.go   # codegraph MCP（可选插件）
│   │   ├── agent/                     # Head 2: 执行头 — 执行 + 采集
│   │   │   ├── agent.go               # Agent 接口 + 步骤调度
│   │   │   ├── executor.go            # 规则引擎 + AI 引导决策树
│   │   │   ├── api.go                 # API 测试执行（HTTP client）
│   │   │   ├── browser/               # 浏览器测试执行
│   │   │   │   ├── mcp_client.go      # Playwright MCP 通信层
│   │   │   │   ├── actions.go         # click/type/navigate 封装
│   │   │   │   └── snapshot.go        # 页面快照 + 截图
│   │   │   ├── recovery.go            # 失败恢复策略
│   │   │   └── evidence.go            # 证据采集（before/after diff、截图、响应）
│   │   └── examiner/                  # Head 3: 审查头 — 评估 + 学习
│   │       ├── examiner.go            # 检查路由 + 裁决合并
│   │       ├── judge.go               # AI 判断（LLM 调用 + 结构化输出）
│   │       ├── learner.go             # 学习逻辑（更新 L1/L2/L3 记忆 + ProjectModel）
│   │       ├── policy.go              # 置信度策略 + Uncertain 3 级降级链
│   │       ├── types.go               # Verdict, CheckResult 等公共类型
│   │       └── checker/               # 确定性检查器（子模块）
│   │           ├── checker.go         # 注册表 + 类型路由
│   │           ├── assertion.go       # 通用断言解析器
│   │           ├── sql.go             # SQL Checker
│   │           ├── http.go            # HTTP Checker
│   │           ├── diff.go            # Diff Checker
│   │           └── enhanced/          # 可选增强
│   │               ├── jsonschema.go  # JSON Schema（gojsonschema）
│   │               └── openapi.go     # OpenAPI 契约（kin-openapi）
│   ├── memory/
│   │   ├── episodic/                 # L1: 情景记忆
│   │   ├── semantic/                 # L2: 语义记忆
│   │   │   ├── store.go
│   │   │   ├── embedding.go          # 自动探测 provider
│   │   │   ├── ollama.go
│   │   │   ├── openai.go
│   │   │   └── fallback.go           # tsvector 兜底
│   │   ├── procedural/               # L3: 程序记忆
│   │   ├── search.go                 # 统一检索（三层降级）
│   │   └── learner.go                # Session 结束时的学习逻辑
│   ├── project/
│   │   ├── loader.go                 # YAML 加载 + env var 插值
│   │   ├── schema.go                 # 项目描述数据结构
│   │   ├── defaults.go               # 零配置默认值
│   │   ├── model.go                  # 项目模型 + 成熟度评分
│   │   └── credentials.go            # 凭证管理（文件 + env var）
│   ├── session/
│   │   ├── lifecycle.go              # run/verify/serve 生命周期
│   │   └── phases.go                 # 阶段转换
│   ├── llm/
│   │   ├── client.go
│   │   ├── claude.go
│   │   ├── openai.go
│   │   └── gemini.go
│   ├── evidence/                     # 证据采集 + EvidenceStore（简化版，无 pub/sub）
│   │   ├── store.go                  # Add/Query/All（线性收集，按需查询）
│   │   ├── types.go
│   │   └── collector.go
│   ├── store/                        # PG 存储层（移植）
│   ├── fixer/                        # 自动修复（移植）
│   ├── report/                       # 报告生成（移植）
│   └── server/                       # HTTP API — serve 模式（移植+重构）
├── config/schema.yaml
├── migrations/V001__cerberus.sql
├── go.mod
├── Makefile
└── README.md
```

### 移植 vs 新建

| 模块 | 来源 | 改动量 |
|------|------|--------|
| `store/` | 移植 | 中（去 Relay 表名，加 verdicts/memory 表；MVP 用 SQLite，C4 迁移 PostgreSQL） |
| `evidence/` | 移植+重构 | 中（简化为 EvidenceStore） |
| `report/` | 移植 | 小（加 verdict 展示） |
| `fixer/` | 移植 | 小（去 Relay 文件约束） |
| `server/` | 移植+重构 | 中（serve 模式 API 端点） |
| `ai/` | 全新 | — |
| `head/scout/` | 全新 | — |
| `head/agent/` | 全新 | — |
| `head/examiner/` | 全新 | — |
| `memory/` | 全新 | — |
| `project/` | 全新 | — |
| `session/` | 大改（三模式替代 7-Phase） | — |
| `llm/` | 全新 | — |

## 12. DB Schema

> **存储策略**：MVP (C1-C3) 使用 SQLite，C4 迁移至 PostgreSQL 以获得 JSONB、GIN 索引、pgvector 支持。
> 以下 schema 展示 **C4 PostgreSQL 目标形态**，V001 migration 实际使用 SQLite 兼容语法。

### V001 — Core Schema (SQLite MVP)

```sql
-- V001__cerberus.sql (SQLite for C1-C3)
-- C4 migration will convert to PostgreSQL types (UUID, JSONB, TIMESTAMPTZ, etc.)

-- 会话
CREATE TABLE sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  mode TEXT NOT NULL CHECK (mode IN ('run', 'verify', 'serve')),
  status TEXT NOT NULL DEFAULT 'running',
  goal TEXT,
  project_name TEXT,
  coverage_pct REAL NOT NULL DEFAULT 0,
  stats JSONB NOT NULL DEFAULT '{}',   -- {total_tokens, ai_calls, steps, ...}
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ
);
CREATE INDEX idx_sessions_status ON sessions(status);
CREATE INDEX idx_sessions_project ON sessions(project_name);

-- 操作追踪
CREATE TABLE traces (
  id BIGSERIAL PRIMARY KEY,
  session_id UUID NOT NULL REFERENCES sessions(id),
  category TEXT NOT NULL,
  target TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'running',
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ,
  metadata JSONB
);
CREATE INDEX idx_traces_session ON traces(session_id);

-- 证据
CREATE TABLE evidence (
  id BIGSERIAL PRIMARY KEY,
  trace_id BIGINT NOT NULL REFERENCES traces(id),
  type TEXT NOT NULL,   -- "screenshot" | "api_response" | "db_snapshot" | "log"
  content JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_evidence_trace ON evidence(trace_id);

-- 裁决
CREATE TABLE verdicts (
  id BIGSERIAL PRIMARY KEY,
  session_id UUID NOT NULL REFERENCES sessions(id),
  trace_id BIGINT NOT NULL REFERENCES traces(id),
  target TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pass', 'fail', 'uncertain', 'skip')),
  confidence REAL NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('judge', 'checker', 'merged')),
  reasoning TEXT,
  suggestions JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_verdicts_session ON verdicts(session_id);
CREATE INDEX idx_verdicts_status ON verdicts(status);

-- L1 情景记忆（30 天自动衰减 — 通过 cleanup job 归档旧记录）
CREATE TABLE memory_episodic (
  id BIGSERIAL PRIMARY KEY,
  session_id UUID NOT NULL REFERENCES sessions(id),
  target TEXT NOT NULL,
  status TEXT NOT NULL,
  verdict JSONB,
  duration INTERVAL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_episodic_target ON memory_episodic(target);
CREATE INDEX idx_episodic_created ON memory_episodic(created_at DESC);

-- L2 语义记忆（embedding 列在 V002 中添加）
CREATE TABLE memory_semantic (
  id BIGSERIAL PRIMARY KEY,
  content TEXT NOT NULL,
  source TEXT NOT NULL,
  tags TEXT[] NOT NULL DEFAULT '{}',
  confidence REAL NOT NULL DEFAULT 0.5,
  project_name TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_semantic_tags ON memory_semantic USING GIN(tags);
CREATE INDEX idx_semantic_search ON memory_semantic USING GIN(to_tsvector('english', content));

-- L3 程序记忆
CREATE TABLE memory_procedural (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  condition TEXT NOT NULL,
  action TEXT NOT NULL,
  effectiveness REAL NOT NULL DEFAULT 0.5,
  usage_count INT NOT NULL DEFAULT 0,
  project_name TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 项目模型版本
CREATE TABLE project_models (
  id BIGSERIAL PRIMARY KEY,
  project_name TEXT NOT NULL,
  version INT NOT NULL DEFAULT 1,
  model TEXT NOT NULL,       -- YAML 格式，schema 版本化
  schema_version INT NOT NULL DEFAULT 1,
  source TEXT NOT NULL,       -- "cognition" | "manual"
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(project_name, version)
);
```

> **数据清理策略**：
> - L1 情景记忆：`cerberus cleanup` 命令归档 30 天前的记录（移到 `memory_episodic_archive`）
> - 证据截图：90 天后自动清理（只保留元数据）
> - sessions：保留最近 100 个，旧记录归档

## 13. 与 relay-test-harness 的关系

Cerberus 是 relay-test-harness 的**精神继承者**，不是 fork：

- relay-test-harness 继续服务于 Relay 平台的特定测试需求
- Cerberus 是独立项目，通用化，开源
- 精选移植已验证的通用模块（store、evidence、report、fixer）
- 核心模块（三头、记忆、项目插件）全新设计

## 14. 跨领域关注点

### 14.1 错误处理与降级矩阵

| 组件 | 故障场景 | 降级策略 |
|------|----------|----------|
| LLM | 不可用 / 超时 / 429 rate limit | 纯 Checker 模式；超时重试 1 次；rate limit 指数退避 |
| Playwright MCP | 进程崩溃 / 连接断开 / 未安装 | 重启 MCP Server（最多 2 次）；放弃浏览器测试，**退回纯 API 测试模式** |
| PostgreSQL | 连接断开 / 查询超时 | 记忆不可用时继续测试（不写入记忆）；查询超时跳过 Checker |
| CodeProvider | 分析失败 / 文件缺失 | 跳过代码认知，仅使用 Surface + Data 维度 |
| KnowledgeSource | cccemory/OpenWolf/codegraph 未安装 | `Available()` 返回 false，直接跳过，零开销；Cerberus 自行探索覆盖 |
| Evidence Bus | channel 满 / 内存不足 | 降级为同步写入（牺牲并发），优先保障证据不丢失 |
| Embedding | Ollama/OpenAI 不可用 | 退回 tsvector 全文检索（永远可用） |

降级状态记录在 Session stats 中，报告展示降级详情。

### 14.2 并发模型

```
Session (1 per CLI invocation)
  ├─ Explorer
  │   ├─ Surface 爬取: 并发 goroutine 池 (max 5)
  │   ├─ Code 分析: 串行（按 provider 顺序）
  │   ├─ Data 分析: 串行（单 DB 连接）
  │   └─ 浏览器操作: 严格串行（单浏览器 tab）
  ├─ Judge
  │   └─ JudgeBatch: 并发 (max 3 goroutines, 受 token budget 限制)
  ├─ Checker
  │   └─ SQL checks: 并发 goroutine 池 (max 10)
  └─ Memory
      └─ 写入: 异步 channel，批量 flush
```

浏览器操作必须串行（Playwright 单 tab 限制），这是主要性能瓶颈。
Judge 的并发受 token budget 控制：剩余 budget 不足时自动降低并发度。

### 14.3 成本控制

```yaml
# .cerberus/project.yaml
settings:
  ai_budget:
    session_total_tokens: 200000   # session 总 token 上限
    per_call_limit: 10000          # 单次 AI 调用上限
    model: "claude-sonnet-4-6"     # 默认模型（可按决策点覆盖）
  cost_alerts:
    warn_at_pct: 80                # 80% 时打印警告
    stop_at_pct: 100               # 100% 时停止 AI 调用
```

每条 Verdict 记录消耗的 token 数。Session 结束时汇总到 `stats.total_tokens`。

### 14.4 安全

| 关注点 | 策略 |
|--------|------|
| 截图脱敏 | 浏览器操作前注入 CSS 隐藏 `input[type=password]`；截图前自动模糊密码字段 |
| 凭证存储 | `.cerberus/credentials.yaml` 在 `.gitignore` 中；支持 env var |
| LLM 数据 | 截图/响应发送前检查是否含 API key pattern（正则匹配），发现则脱敏 |
| DB 连接 | 只读连接用于查询，写入仅限于 Cerberus 自身的表 |

### 14.5 测试数据隔离

- **API 测试**：操作创建真实数据，session 结束时通过 cleanup 函数回滚（用户需在 project.yaml 中定义 cleanup 脚本）
- **before/after diff**：每个 trace 记录操作前的 DB snapshot，diff 时只比较相关表
- **多 session 并发**：serve 模式下不同 session 操作不同的 test tenant（通过 actors 隔离）

### 14.6 测试框架自身的测试

Cerberus 自身的测试策略：

| 层级 | 方法 | 覆盖范围 |
|------|------|----------|
| 单元测试 | Go standard `testing` | AI Driver prompt 构造、TokenBudget 计算、SQL assertion 解析、Memory 检索 |
| 集成测试 | 测试 PG 实例 + mock LLM | Store 读写、Evidence Bus 发布订阅、Checker SQL 执行 |
| E2E 测试 | 本地 HTTP server + Playwright MCP | 完整 `cerberus run` 流程（使用 mock 应用） |
| Mock LLM | 固定响应的 LLM Client | 不依赖真实 LLM API，可重复运行 |

Mock LLM 是关键：所有测试必须能在无 LLM API key 的环境下运行。

## 15. 实施阶段（概要）

详细计划在 implementation plan 中制定。预估阶段：

### MVP 定义

**最小可用版本**：只支持 API 测试（无浏览器），验证核心架构：

- CLI：`cerberus run --url + --goal` + `cerberus init`
- AI Driver：单决策点（Judge），固定 prompt 模板
- Checker：SQL 快捷检查 + Go 函数检查
- Memory：L1 情景记忆（仅 PG 表，无 embedding）
- Store：移植 relay-test-harness store
- 报告：JSON 报告（无 HTML）

MVP 目标：用户可以 `cerberus run --url http://localhost:8080 --goal "测试所有 API"`，
框架能自动发现端点、发送请求、用 AI 判断响应、生成报告。

### 阶段规划

```
C1 ──→ C1.5 ──→ C2a ──→ C2b ──→ C3 ──→ C4 ──→ C5 ──→ C6
(骨架)  (验证)    (API)    (浏览器) (三头)  (记忆)  (会话)  (Server)
                   ↑                    ↑
                   └─── MVP ───────────┘
```

| 阶段 | 内容 | 依赖 | 工期 | 风险 |
|------|------|------|------|------|
| C1 | 骨架 + CLI (init/run) + 项目插件 + LLM Client + AI Driver 骨架 | 无 | 3 周 | 低 |
| C1.5 | **核心假设验证**：用 10 个真实 API 端点 + 人工标注期望，验证 Judge 准确率 | C1 | 1 周 | **关键**（决定后续方向） |
| C2a | Explorer API 探索（无浏览器）+ CodeProvider (gostatic/openapi) | C1.5 | 1.5 周 | 低 |
| C2b | Explorer 浏览器探索（Playwright MCP 集成） | C2a | 2 周 | **高**（Go 无官方绑定） |
| C3 | Judge 头 + Checker 头 + Arbitrator 裁决合并 | C2a | 2 周 | 中 |
| C4 | 记忆系统（L1+L2+L3 + embedding 三层降级）+ **SQLite → PostgreSQL 迁移** | C1 | 2.5 周 | 中（DB 迁移 + embedding 集成） |
| C5 | 会话管理 + Store + EvidenceStore + 报告（移植） | C1 | 2 周 | 低（移植为主） |
| C6 | Server (serve 模式 API) + 集成测试 + Mock LLM + 文档 | C3+C4+C5 | 2 周 | 低 |

**总计约 12-16 周**。

#### C1.5 核心假设验证（Go/No-Go 里程碑）

> **为什么需要 C1.5**：C1 交付的是骨架——Store 能跑、LLM Client 能调 API、AI Driver 能解析 JSON，
> 但没有一个"头"能真正测试任何东西。如果 AI 判断 API 响应的准确率低于 80%，
> 整个方向需要调整。C1.5 用最小代价验证最危险的假设，避免 6 周后发现方向错误。

**验证方法**：
1. 准备 10 个真实 API 端点（含正常 + 异常响应），人工标注 pass/fail 期望
2. 用 C1 的 AI Driver + Judge prompt 模板对每个响应做判断
3. 计算：准确率、误报率、漏报率
4. **通过标准**：准确率 ≥ 85%，误报率 < 10%

**Go/No-Go 决策**：
- ✅ 通过 → 继续按计划推进 C2a
- ⚠️ 准确率 70-85% → 优化 prompt 模板 + 增加上下文注入，重测一次
- ❌ 准确率 < 70% → 暂停，重新评估 AI 判断的可行性，考虑备选方案

### 并行策略

- C1.5 通过后，**C2b 和 C3 可并行**（浏览器 vs AI 判断独立）
- C4 和 C5 **可与 C2b/C3 并行**（独立于三头逻辑）
- C6 必须等 C3+C4+C5 全部完成

### 风险缓解

| 风险 | 缓解策略 |
|------|----------|
| Playwright MCP 性能不足 | C2b 第一周做 POC 验证；不行则评估 playwright-go |
| LLM 成本超预期 | MVP 阶段即实现 TokenBudget；默认 200K/session |
| 记忆系统检索质量差 | MVP 只用 L1 情景记忆；L2/L3 逐步迭代 |
