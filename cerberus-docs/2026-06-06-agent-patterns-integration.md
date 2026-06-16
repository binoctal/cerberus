# Cerberus AI Agent 模式集成提案（v3 — Review 修订版）

> **v1 → v2 变更**：经过 brainstorming review，修订了 4 个关键设计决策
> - 反思分类：字符串匹配 → LLM 同时输出 category
> - Self-Refine：同模型自评 → 双模型（Sonnet 判断 + Haiku 批评）
> - 反思注入：全量注入 → Skill 式 glob 匹配调度
> - ToT 评估：纯 LLM 评分 → AI 为主(70%) + 确定性为辅(30%)
> - 新增全局策略模板库，新项目自动继承基础反思
> - 移除 session token budget：成本由结构参数控制，API spending limits 作安全网
>
> **v2 → v3 变更**：第三轮 review — 覆盖记忆管理/学习适应/评估监控/反思四维度
> - 双向学习：仅失败反思 → 失败反思 + 成功经验（同一 LLM 调用）
> - Prompt 管理：Go const 硬编码 → Skill 式注册中心（embed.FS 默认 + 项目级覆盖）
> - 反思质量门控：存入 L3 前校验（diagnosis/strategy 非空、pattern 可编译）
> - 记忆容量：不设硬上限，依赖 effectiveness 自然淘汰 + 过期标记（90 天）
> - 新增 §10 跨领域关注点：可观测性、配置完整性、错误恢复、测试策略、并发安全
> - Sub-Agent 并行架构（§5b）：Agent 头 DAG 并行执行拓扑，默认串行退化为零开销

## Context

Cerberus 是面向 SaaS 开发者的通用 AI 测试框架（Go 实现），当前处于 C1 骨架完成阶段。为提升框架的认知、执行、评估、学习能力，深入研究了 7 个 AI Agent 项目，提取 4 大设计模式按架构适配。

**核心结论**：采用 ReAct（执行循环）+ Reflexion（学习机制）+ Self-Refine（判断迭代）+ ToT（C4 深度规划）。LATS MCTS 排除（~400K token/任务，结构参数无法有效约束）。

### 设计原则：该思考的让 AI 思考，该确定的让规则确定

| 阶段 | 确定性 vs AI | 原因 |
|------|-------------|------|
| Scout 规划（认知+规划） | **AI 为主** | 需要推理和理解 |
| Agent 执行（规则+引导） | **确定性为主**（~70%） | 操作大多是确定性的 |
| Examiner 裁决（检查+判断） | **确定性优先** | Checker > Judge |
| ToT 候选评估（规划质量） | **AI 为主**（70%） | 规划质量需要语义理解 |

---

## 1. 模式到组件的映射

| 模式 | 目标头 | 决策点 | 采纳的核心机制 |
|------|--------|--------|----------------|
| **ReAct** | Agent（执行头） | Steer + Recover | Thought-Action-Observation 循环、LLM 输出解析兜底 |
| **Sub-Agent** | Agent（执行头） | Execute | DAG 并行执行拓扑、goroutine 调度、状态共享、错误隔离 |
| **Reflexion（简化版）** | Examiner（审查头） | Learn | 失败反思生成 → L3 程序记忆 → Skill 式 glob 匹配注入 |
| **Self-Refine** | Examiner（审查头） | Judge | 双模型判断（Sonnet+Haiku），高置信度提前终止 |
| **LangGraph Reflection** | 架构验证 | — | 确认 Checker-vs-Judge 分离设计正确，无需新代码 |
| **ToT (Beam Search)** | Scout（侦察头） | Plan（深度模式） | BFS beam search + AI 为主评估 |
| **LATS (MCTS)** | — | — | ❌ 不采纳：~400K token/任务，结构参数无法有效约束 |

**不采纳的设计**：
- ReAct 的无限增长 prompt → 用 `ContextEntry` + 相关性排序替代
- Reflexion 原始版无持久化记忆 → L3 持久化 + effectiveness 跟踪
- LATS 的 MCTS → 测试有密集确定性奖励，不需要蒙特卡洛模拟

---

## 2. ReAct 执行循环 — Agent 头（C2b 阶段）

### 设计要点

```
规则引擎匹配（~70%，零 token）
  → 成功 → 返回 StepResult
  → 失败 → 进入 ReAct 循环

ReAct 循环（最多 3 次 Steer 尝试）：
  Thought: AI 分析当前页面/API 状态
  Action:  选择具体操作（click/type/navigate/api_request）
  Observation: 执行操作，采集证据
  → 成功 → 返回
  → 失败 → 更新状态，重试
  → 3 次均失败 → 降级为 skipped
```

### 关键文件

| 文件 | 职责 |
|------|------|
| `internal/head/agent/executor.go` | ReAct 循环主逻辑 + 规则引擎调度 |
| `internal/head/agent/recovery.go` | 失败恢复（Recover 决策点）+ Skill 式反思注入 |
| `internal/head/agent/prompts.go` | Steer/Recover prompt 模板 |

### Prompt 模板

```go
const promptSteerSystem = `You are a test execution agent. You observe the current page/API state and decide the next action.

RULES:
- Choose ONE action: click, type, navigate, api_request, wait.
- Be specific: use exact selectors from the snapshot.
- If the page shows an error, choose a recovery action.
- Never fabricate elements. Only reference visible elements.`

const promptSteerOutput = `JSON: {
  "reasoning": "why this action",
  "action": {"type": "click|type|navigate|api_request|wait", "target": "...", "value": "..."}
}`

// 使用方式：通过 promptBuilder 合并为单个 prompt 传给 Driver.Decide()
// prompt := ai.NewPrompt().System(promptSteerSystem).Context(snapshot).Task(goal).Output(promptSteerOutput).Build()
```

### LLM 输出解析兜底

当 `ParseStructuredOutput` 失败时，从第一行提取意图，构造最小化 Action。避免执行卡死。

---

## 3. Reflexion 学习循环 — Examiner 头（C3 + C4 阶段）

### 设计要点

```
Session 结束 → 收集所有 fail/uncertain/pass Verdict
  → 单次 LLM 调用批量生成反思（失败反思 + 成功经验，每条含 category + type）
  → 质量门控：diagnosis 非空、strategy ≥10 字符、condition_pattern 可编译
  → 存入 L3 程序记忆（effectiveness=0.5, created_at=now）
  → 后处理：按 category+type 去重，每类别保留最多 3 条（按 diagnosis 特异性排序）
  → 无硬容量上限：依赖 effectiveness 自然淘汰（<0.2 归档）

下次 Session → Skill 式调度注入
  → glob 匹配 condition_pattern 与当前操作目标
  → 失败反思：注入 ≤2 条（匹配失败场景的恢复策略）
  → 成功经验：注入 ≤1 条（匹配成功场景的最佳实践）
  → 按 effectiveness 降序选取
  → 使用后根据成败更新 effectiveness（EMA α=0.3）
  → 过期：created_at 超 90 天且 effectiveness < 0.5 → 标记 stale（不注入但保留）
```

> **为什么单次调用**：原始设计按类别分别调用（5 类别 = 5 次 LLM ≈ 20K token）。
> 改为单次调用传入所有 Verdict，让 LLM 一次性输出多条反思（含不同 category），
> 成本从 ~20K 降至 ~8K（含双向学习后略有增加），且 LLM 可交叉分析不同结果之间的关联。

### 反思生成（单次 LLM 批量输出，同时含 category）

```go
const promptReflectionSystem = `You are a test learning agent. Analyze ALL test results below and generate concise, actionable reflections.

RULES:
- Output a JSON array of reflections.
- For FAILURES: generate root cause analysis + recovery strategy (type=failure).
- For SUCCESSES: extract key practices worth repeating (type=success).
- Focus on root causes, not symptoms.
- Each reflection: specific condition_pattern + concrete strategy + category + type.
- Maximum 2 sentences per reflection.
- condition_pattern must be a glob pattern using * for wildcards (e.g. POST /api/v1/* returned 401, * returned 5??).
- Pick the most specific category from the list below.`

const promptReflectionOutput = `JSON array: [
  {
    "type": "failure | success",
    "diagnosis": "root cause or key practice in 1 sentence",
    "strategy": "recovery action or repeatable approach",
    "condition_pattern": "glob pattern for matching future scenarios",
    "category": "timeout_recovery | auth_failure | endpoint_not_found | server_error | ambiguous_result | general_failure"
  }
]`
```

> **glob pattern 规范**：使用 `github.com/gobwas/glob` 库匹配。支持 `*`（任意字符）、`?`（单字符）、
> `**`（跨路径分隔符）、`[...]`（字符类）。LLM 生成的 pattern 在存入 L3 前需通过
> `glob.Compile()` 验证——编译失败则回退为 `*`（匹配所有）。

### 反思质量门控

存入 L3 前的确定性校验，零 token 开销：

```go
// qualityGate validates a reflection before L3 storage.
func qualityGate(r ProceduralMemory) bool {
    if r.Diagnosis == "" || len(r.Strategy) < 10 { return false }
    if _, err := glob.Compile(r.Condition); err != nil { return false }
    return true
}
```

未通过门控的反思静默丢弃，不重试。降低 L3 噪声。

### Skill 式反思调度（确定性 glob 匹配，零 token 开销）

> **Target 格式规范**：`currentTarget` 传入格式为 `"<METHOD> <PATH> returned <STATUS>"`。
> 例如 `"POST /api/v1/users returned 401"`、`"GET /api/v1/users returned 200"`。
> 无状态码时为 `"<METHOD> <PATH>"`（如浏览器操作）。condition_pattern 按此格式编写。

```go
// internal/head/examiner/strategy_matcher.go

import "github.com/gobwas/glob"

const (
    maxFailureStrategies = 2  // 每次注入最大失败反思数
    maxSuccessStrategies = 1  // 每次注入最大成功经验数
)

// MatchStrategies 扫描 L3 反思库，glob 匹配当前操作目标，
// 按 type 分组，按 effectiveness 降序返回。确定性，零 token。
// currentTarget 格式: "<METHOD> <PATH> [returned <STATUS>]"
func MatchStrategies(ctx context.Context, store *Store, currentTarget string) ([]ProceduralMemory, error) {
    all, err := store.GetProceduralByEffectiveness(ctx, 0.2, 20)
    if err != nil { return nil, err }

    var failures, successes []ProceduralMemory
    for _, r := range all {
        g, err := glob.Compile(r.Condition)
        if err != nil { continue } // 跳过无效 pattern
        if g.Match(currentTarget) {
            if r.Type == "success" {
                successes = append(successes, r)
            } else {
                failures = append(failures, r)
            }
        }
    }
    if len(failures) > maxFailureStrategies { failures = failures[:maxFailureStrategies] }
    if len(successes) > maxSuccessStrategies { successes = successes[:maxSuccessStrategies] }
    return append(failures, successes...), nil
}
```

### 注入格式（仅注入匹配的策略）

```
## Learned Strategies (matched to current context)
- [failure] When POST /api/v1/* returned 401: Refresh auth token before retry (effectiveness: 75%)
- [success] For GET /api/v1/users: Always include pagination params for list endpoints (effectiveness: 80%)
```

### 全局策略模板库

首次 `cerberus init` 时写入 L3 数据库，新项目不从零开始：

> 模板编译时嵌入二进制（`embed.FS`），不生成单独的 `.yaml` 文件。
> `cerberus init` 调用时从嵌入数据写入 `memory_procedural` 表，
> 与主设计 §2.4 的 `cerberus init` 产出（`project.yaml`、`credentials.yaml`、`invariants/`）一致，
> 不新增文件。

```yaml
# internal/head/examiner/embedded/templates.yaml（编译时嵌入）
templates:
  - category: timeout_recovery
    condition: "* returned timeout"
    action: "Increase timeout to 30s, add retry with exponential backoff"
    effectiveness: 0.5
  - category: auth_failure
    condition: "* returned 401"
    action: "Refresh auth token before retry"
    effectiveness: 0.5
  - category: auth_forbidden
    condition: "* returned 403"
    action: "Check actor role permissions, try with higher-privileged actor"
    effectiveness: 0.5
  - category: server_error
    condition: "* returned 5??"
    action: "Retry up to 3 times with 5s delay, then mark as server issue"
    effectiveness: 0.5
```

### Effectiveness 生命周期

```
生成 → effectiveness = 0.5（无论 failure/success）
使用后成功 → effectiveness = 0.7 × old + 0.3 × 1.0
使用后失败 → effectiveness = 0.7 × old + 0.3 × 0.0
持续 ≥ 0.7 且 usage ≥ 5 → 标记 pinned
跌至 < 0.2 → 归档（不删除，不再注入）
created_at 超 90 天 且 effectiveness < 0.5 → 标记 stale（不注入但保留）

容量策略：不设硬上限。
- effectiveness 机制已提供自然淘汰（差的归档、好的保留）
- stale 标记防止过期反思干扰新 session
- 如未来确需限制，加软限制（警告阈值），不建议硬拒绝写入

全局模板的特殊处理：
- 全局模板 effectiveness 初始 0.5，与项目级反思同等对待
- 项目级反思会覆盖同类别的全局模板（优先匹配项目级）
- 全局模板 pinned by default：pinned 只防止归档删除，不防止跳过
  - effectiveness < 0.2 时 MatchStrategies 仍会跳过它（不注入 prompt）
  - 但记录保留在 L3 表中，effectiveness 可因后续成功使用而回升
```

### 关键文件

| 文件 | 职责 |
|------|------|
| `internal/head/examiner/learner.go` | 反思生成 + effectiveness 更新 |
| `internal/head/examiner/strategy_matcher.go` | **新文件**：Skill 式 glob 匹配调度 |
| `internal/head/examiner/prompts.go` | 反思生成/注入 prompt 模板 |
| `internal/store/memory.go` | 扩展 L3 CRUD（`ProceduralMemory` 类型 + `GetProceduralByEffectiveness` 等）— C4 新增 |
| `migrations/V002__reflections.sql` | memory_procedural 添加 category、type、created_at 列 |
| `internal/head/examiner/embedded/templates.yaml` | **新文件**：编译时嵌入的全局策略模板（`embed.FS`） |

---

## 4. Self-Refine 判断循环 — Examiner 头（C3 阶段）

### 设计要点（双模型）

> **与主设计 §1.4 Uncertain 降级链的关系**：Self-Refine 是降级链 Level 1 的具体实现。
> 初始判断 = Level 1 首次 Judge；自批评 = Level 1 "换 prompt 重试"。
> 如果 Self-Refine 后仍 uncertain，才进入 Level 2（Checker-only）和 Level 3（待审）。

```
初始判断（Sonnet）
  → confidence ≥ 0.9 且 status ≠ uncertain → 直接返回（省 ~50% token）
  → confidence < 0.9 或 status = uncertain → 进入自批评（= Level 1 重试）

自批评（Haiku — 不同模型，不同盲点）
  → 检查常见判断错误：假阳性、存在性vs正确性混淆、过自信
  → 发现错误 → 修正 Verdict
  → 确认无误 → 保持原判

自批评后仍 uncertain → 进入 Level 2（Checker-only）→ Level 3（待审）
达到批评次数上限 → 返回初始判断（不降级为无批评）
```

### 双模型设计

```go
// internal/head/examiner/judge.go

type Judge struct {
    judgeDriver   *ai.Driver  // 主模型（Sonnet）初始判断
    criticDriver  *ai.Driver  // 批评模型（Haiku）自审 — 更省钱，不同盲点
    critiqueCount int         // 当前 session 已执行批评次数（session 级上限）
    maxCritiques  int         // session 级批评次数上限（结构参数，默认 1；可配置为 per-verdict 模式）
}

func (j *Judge) judge(ctx context.Context, ev Evidence, expectation string) (*JudgeResult, error) {
    // Phase 1: 初始判断 — Sonnet
    var result JudgeResult
    if err := j.judgeDriver.Decide(ctx, judgePrompt, &result); err != nil {
        return nil, err
    }

    // Phase 2: 早停
    if result.CorrectnessConfidence >= 0.9 && result.Status != "uncertain" {
        return &result, nil
    }

    // Phase 3: 自批评 — Haiku
    // session 级控制：整个 session 最多 maxCritiques 次批评（默认 1）
    // 未来可扩展为 per-verdict 模式（每个 uncertain verdict 独立计数）
    if j.critiqueCount >= j.maxCritiques {
        return &result, nil // 达到批评次数上限
    }

    var critique CritiqueResult
    if err := j.criticDriver.Decide(ctx, critiquePrompt, &critique); err != nil {
        return &result, nil // LLM 调用失败，返回初始判断
    }
    j.critiqueCount++

    if !critique.IssuesFound { return &result, nil }

    // Phase 4: 修正
    result.Status = critique.SuggestedStatus
    result.CorrectnessConfidence = critique.SuggestedConfidence
    result.SelfCritique = critique.Critique
    return &result, nil
}
```

### Prompt 模板

**初始判断（Sonnet）：**
```go
const promptJudgeSystem = `You are a test verdict judge. Evaluate test evidence against expectations.

RULES:
- Status: pass, fail, uncertain, skip.
- Separate existence_confidence (does it exist?) from correctness_confidence (does it work?).
- existence_confidence high when you see a real response.
- correctness_confidence high ONLY when response matches expectations.
- When uncertain, explain what evidence would resolve ambiguity.
- Never give correctness_confidence > 0.9 without seeing response body.`
```

**自批评（Haiku）：**
```go
const promptJudgeCritic = `You are a verdict quality reviewer. Check the initial verdict below for common errors.

COMMON ERRORS:
1. False positive: verdict says "pass" but evidence only partially matches.
2. Existence vs correctness confusion: endpoint exists (200) but returns wrong data.
3. Missing edge cases: verdict ignores boundary conditions.
4. Overconfidence: high confidence without sufficient evidence.

Initial verdict: %s
Evidence: %s
Expectation: %s

Output: {"issues_found": bool, "critique": "...", "suggested_status": "...", "suggested_confidence": 0.0}`
```

> 批评 prompt 必须注入初始 Verdict + Evidence + Expectation，否则 Haiku 无法审查。
> 批评结果不直接覆盖原判，而是返回建议，由 `judge()` 方法决定是否采纳。

### 关键文件

| 文件 | 职责 |
|------|------|
| `internal/head/examiner/judge.go` | 双模型判断循环（Sonnet 初始 + Haiku 批评）|
| `internal/head/examiner/policy.go` | 置信度策略 + Uncertain 3 级降级链 |
| `internal/head/examiner/examiner.go` | 检查路由 + 裁决合并（确定性 > AI）|

---

## 5. ToT 深度规划 — Scout 头（C4 阶段）

### 设计要点

```
Scout.Plan（深度模式，成本由结构参数控制）
  Step 1: Propose — LLM 生成 n 个候选测试方向（n=5）
  Step 2: Evaluate — AI 为主(70%) + 确定性为辅(30%)
    AI: LLM 评估策略完整性、风险聚焦、测试多样性
    确定性: 端点/页面覆盖率（table-stakes 门槛）
  Step 3: Select — 保留 top-k（k=3）
  重复 Step 1-3 共 3 轮
  LLM 调用失败（API 错误等）→ 停止搜索，返回当前最优
```

### 成本控制：结构参数，非 token 预算

Cerberus 不设 session token budget。成本由**结构参数**天然限制：

```go
const (
    totBeamWidth = 3  // 每步保留 top-3 候选
    totGenerateN = 5  // 每步生成 5 个候选
    totMaxSteps  = 3  // 最多 3 轮 propose-evaluate-select
)
```

**为什么不用 token budget**：
- 结构参数已决定 token 消耗天花板（理论上限 ~180K，实际 1-2 轮收敛 ~60-120K）
- 预算耗尽导致 skip/incomplete → 用户花了钱但没拿到完整结果
- LLM API 层（Anthropic/OpenAI spending limits）已提供成本上限
- 可选安全网：`--max-tokens` 高级 flag，CI 场景按需启用

### 为什么 AI 为主

ToT 的价值在于 AI 探索多样性和语义评估。覆盖率只是门槛指标——一个覆盖 90% 端点但全是 happy path 的计划，远不如覆盖 60% 但含边界条件和错误场景的计划。如果确定性评分占主导，beam search 退化为贪心搜索，失去引入 ToT 的意义。

### 评估权重

```go
// AI 为主 70% + 确定性为辅 30%
// evaluate prompt 注入已知端点列表，让 LLM 能评估覆盖率
endpointSummary := formatEndpoints(model) // e.g. "POST /api/v1/users, GET /api/v1/users/:id, ..."
candidates[i].Score = llmScore.Score/10.0*0.7 + coverageScore*0.3
```

### 核心流程

```go
func (t *ToTPlanner) Plan(...) (*TestPlan, error) {
    candidates := []PlanCandidate{{Description: goal}}
    for step := 0; step < t.maxSteps; step++ {
        var expanded []PlanCandidate
        for _, c := range candidates {
            proposals := t.propose(ctx, t.driver, c, model, t.generateN)
            expanded = append(expanded, proposals...)
        }

        scored, err := t.evaluate(ctx, t.driver, expanded, model)
        if err != nil {
            break // API 错误等，停止搜索返回当前最优
        }
        sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
        if len(scored) > t.beamWidth {
            scored = scored[:t.beamWidth]
        }
        candidates = scored
    }
    return bestCandidate(candidates).ToTestPlan(), nil
}
```

### Prompt 模板

**Propose：**
```go
const promptToTPropose = `You are a test planning agent. Given a project model and a test goal, propose %d different test strategies.

Each strategy should:
- Focus on a different aspect (e.g., happy path, error handling, edge cases, security)
- Cover distinct endpoints/pages from the project model
- Be completable within 30 minutes

Project Model:
%s

Test Goal: %s

List %d strategies, each with a brief description and the key test cases it would include.`
```

**Evaluate（AI 为主，注入端点上下文）：**
```go
const promptToTEvaluate = `Rate this test strategy on a scale of 1-10.

Focus on STRATEGY QUALITY (this is the primary criterion):
- Risk focus: Does it target high-risk, high-impact areas?
- Completeness: Does it cover happy path AND error cases AND edge cases?
- Diversity: Does it test different types of failures?
- Efficiency: Can it be executed within time constraints?

Known project endpoints (for coverage reference):
%s

Strategy: %s

Output ONLY: {"score": N, "reasoning": "brief explanation"}`
```

### 关键文件

| 文件 | 职责 |
|------|------|
| `internal/head/scout/planner.go` | DirectPlanner（C2a）+ ToTPlanner（C4）|
| `internal/head/scout/tot.go` | ToT beam search 核心逻辑 |

---

## 5b. Sub-Agent 并行架构 — Agent 头（C4+ 阶段）

> **设计定位**：Sub-agent 不是独立的 agent 模式，而是 Agent 头的**执行拓扑升级**。
> 当前 ReAct 是单线程串行执行；Sub-agent 将其扩展为有向无环图（DAG）并行执行。
> 默认 `max_concurrent_agents = 1`（退化为串行，零额外复杂度），渐进启用。

### 触发条件

```
Scout 生成 TestPlan（含 N 个 TestCase）
  → Orchestrator 分析 TestCase 间依赖关系
  → 构建执行 DAG
  → max_concurrent_agents = 1 → 串行执行（当前行为）
  → max_concurrent_agents > 1 → 并行执行（新行为）
```

### 架构总览

```
Scout.Plan → TestPlan{Cases: [TC1, TC2, TC3, TC4, TC5]}
                    ↓
              Orchestrator
                ├── 依赖分析 → DAG
                │   TC1(Login) → [TC2(Create), TC3(List)] → TC4(Update) → TC5(Delete)
                │                  ↑ 并行                    ↑ 串行
                ├── SubAgent Pool (bounded by max_concurrent)
                │   [Worker1: TC2] [Worker2: TC3] ... 排队等待
                ├── 状态共享（SessionContext）
                └── 结果收集 → ResultAggregator
                    ↓
              Examiner.Judge（逐 verdict 或批量）
```

### 核心类型

```go
// internal/head/agent/orchestrator.go

// Task 由 TestCase 派生，Orchestrator 的调度单位
type Task struct {
    ID            string
    Goal          string
    Target        string    // endpoint/page
    Dependencies  []string  // 依赖的 Task ID（必须先完成）
    Type          TaskType  // api, ui, security（预留专项 agent）
}

// Orchestrator 管理子 agent 生命周期
type Orchestrator struct {
    maxConcurrent int                        // 并发上限
    factory       func(ctx context.Context, task Task) *SubAgent
    sharedCtx     *SessionContext             // 跨 agent 共享状态
}

// SubAgent 执行单个 Task，拥有独立 ReAct 循环
type SubAgent struct {
    id     int
    task   Task
    driver *ai.Driver     // 独立 Driver（独立 token 计数）
    react  *ReActLoop     // 独立 ReAct 循环
    result chan StepResult
}

// SessionContext 跨所有子 agent 的共享只读状态
type SessionContext struct {
    AuthTokens   *TokenManager          // 线程安全，自动刷新
    ProjectModel *ProjectModel          // 只读
    Reflections  []ProceduralMemory     // L3 快照（只读，session 结束后统一写回）
    TokenBudget  *atomic.Int64          // 可选，仅 --max-tokens 启用时非 nil；否则为 nil
}
```

### 依赖分析（DAG 构建）

Orchestrator 根据 TestCase 的目标端点和操作类型推断依赖：

```go
// 简单规则推断依赖（无需 LLM）：
// 1. POST /resource → 依赖同 resource 的 GET/PUT/DELETE（先创建才能操作）
// 2. POST /auth/login → 依赖它的大部分测试（需先认证）
// 3. 同一 resource 的操作保持顺序：POST → GET → PUT → DELETE
// 4. 无依赖关系的 TestCase → 可并行
```

> 如果 Scout 在 TestPlan 中已标注 `depends_on` 字段，直接使用；
> 否则 Orchestrator 按上述规则自动推断。

### 并发模型

```go
func (o *Orchestrator) Execute(ctx context.Context, plan *TestPlan) ([]StepResult, error) {
    dag := buildDAG(plan.Cases)
    
    // 拓扑排序 + 分层调度
    sem := make(chan struct{}, o.maxConcurrent) // 信号量控制并发
    var results []StepResult
    
    for layer := range dag.TopologicalLayers() {
        var wg sync.WaitGroup
        layerResults := make([]StepResult, len(layer))
        
        for i, task := range layer {
            wg.Add(1)
            sem <- struct{}{} // 获取 slot
            go func(idx int, t Task) {
                defer wg.Done()
                defer func() { <-sem }() // 释放 slot
                
                agent := o.factory(ctx, t)
                layerResults[idx] = agent.Run(ctx)
            }(i, task)
        }
        wg.Wait()
        results = append(results, layerResults...)
    }
    return results, nil
}
```

### 状态共享策略

| 状态类型 | 共享方式 | 原因 |
|----------|----------|------|
| Project Model | 只读共享 | 静态，无冲突 |
| Auth Tokens | 共享 + 自动刷新 | `TokenManager` 线程安全，一个 goroutine 负责刷新 |
| Test Results | 隔离 | 不同子 agent 测不同端点，无交集 |
| L3 反思 | 只读快照 | 每个 sub-agent 启动时从 L3 拍照，写入在 session 结束后统一处理 |
| Prompt 模板 | 只读共享 | PromptRegistry 无状态 |
| Token 预算 | 原子计数器 | `--max-tokens` 跨所有 sub-agent 共享（可选） |

### 错误隔离

| 场景 | 处理 |
|------|------|
| 单个 sub-agent 失败 | 标记该 Task 为 skip/failed，其他继续 |
| 依赖的 Task 失败 | 下游 Task 全部 skip（级联跳过） |
| API rate limit (429) | Orchestrator 暂停所有 sub-agents → 退避 → 恢复 |
| Context 取消 | 所有 sub-agents 通过 `ctx.Done()` 优雅退出 |
| Sub-agent 超时 | 单 Task 超时（默认 5min），不阻塞其他 |

### 与现有 Agent 模式的集成

| 模式 | Sub-agent 适配 |
|------|---------------|
| **ReAct** | 每 sub-agent 独立 ReAct 循环（独立 maxSteerAttempts） |
| **Reflexion** | 共享 L3 快照（只读），session 结束后 Orchestrator 统一调用 Learn |
| **Self-Refine** | 每 sub-agent verdict 独立经过 Judge |
| **ToT** | Scout ToT 规划不变（发生在 Orchestrator 之前） |
| **Prompt Registry** | 所有 sub-agent 共享同一 PromptRegistry 实例 |

### 成本影响

| 场景 | 并发数 | 耗时 | Token 总量 |
|------|--------|------|------------|
| 串行（默认） | 1 | T | X |
| 2 并行 | 2 | ~T/2 | ~X（端点不重叠，总量近似） |
| 4 并行 | 4 | ~T/4 | ~X（同上） |
| Rate limit 触发 | N | > T | > X（重试开销） |

> 并行不增加 token 总量（每个端点只测一次），但增加**瞬时吞吐**。
> Rate limit 是实际瓶颈：建议 `max_concurrent_agents ≤ 3` 避免触发 API 限流。

### 关键文件

| 文件 | 职责 |
|------|------|
| `internal/head/agent/orchestrator.go` | **新文件**：DAG 构建 + 并发调度 + 结果收集 |
| `internal/head/agent/subagent.go` | **新文件**：SubAgent 封装（独立 ReAct + Driver） |
| `internal/head/agent/session_context.go` | **新文件**：SessionContext 共享状态 + TokenManager |
| `internal/head/agent/executor.go` | 现有，适配为 SubAgent 的内部调用 |


---

## 6. Prompt 注册中心（Skill 式管理）

> **设计哲学**：Prompt 是 Cerberus 的核心资产，应版本化、可发现、可覆盖。
> 从 Go const 硬编码迁移到 **embed.FS 默认 + 项目级覆盖**，类似 §3 策略模板的 Skill 模式。

### 架构

```go
// internal/ai/prompts/registry.go

type PromptRegistry struct {
    defaults fs.FS              // 编译时嵌入的默认 prompts（embed.FS）
    overlay  fs.FS              // 可选：项目级 .cerberus/prompts/ 覆盖
    cache    map[string]*PromptTemplate
}

type PromptTemplate struct {
    ID      string   // steer, recover, judge, judge_critic, reflection, tot_propose, tot_evaluate
    Version string   // 语义化版本 "1.0.0"
    System  string   // system 段
    Task    string   // task 段（可含 {{.param}} 占位符，text/template 语法）
    Output  string   // output format 段
    Params  []string // 预期参数名
}

// Load 加载优先级：overlay > defaults。overlay 缺失项自动回退 defaults。
func (r *PromptRegistry) Load(id string) (*PromptTemplate, error)

// Render 将参数注入模板，返回完整 prompt 字符串
func (r *PromptRegistry) Render(id string, params map[string]string) (string, error)
```

### 目录结构

```
internal/ai/prompts/
  embedded/              # Go embed.FS — 编译时嵌入的默认 prompt
    steer.yaml           # ReAct Steer
    recover.yaml         # 失败恢复
    judge.yaml           # 初始判断（Sonnet）
    judge_critic.yaml    # 自批评（Haiku）
    reflection.yaml      # 反思生成（失败+成功）
    tot_propose.yaml     # ToT 候选生成
    tot_evaluate.yaml    # ToT 候选评估
  registry.go            # PromptRegistry 加载/渲染/覆盖
```

### 覆盖机制

用户可在项目目录 `.cerberus/prompts/` 放置同名 YAML 覆盖默认 prompt：

```yaml
# .cerberus/prompts/steer.yaml — 项目级覆盖示例
id: steer
version: "1.0.0"
system: |
  You are a test execution agent for a healthcare SaaS...
task: |
  Current state: {{.snapshot}}
  Goal: {{.goal}}
output: |
  JSON: {"reasoning": "...", "action": {...}}
params: [snapshot, goal]
```

- overlay 优先，缺失项回退 defaults（不会因少一个文件而 break）
- 版本不匹配时输出 warning 日志
- 默认 prompts 始终内嵌在二进制中，零配置即可运行

### Prompt 构建器保留

`promptBuilder` 保留为内部组装工具，新增 `Reflections` 段：

```go
// Build 顺序：System → Context → Reflections → Task → Output Format
// Reflections 段由 MatchStrategies 结果自动注入
```

### 关键文件

| 文件 | 职责 |
|------|------|
| `internal/ai/prompts/registry.go` | **新文件**：PromptRegistry 加载/渲染/覆盖 |
| `internal/ai/prompts/embedded/*.yaml` | **新文件**：编译时嵌入的默认 prompt 模板 |
| `internal/ai/prompt.go` | 保留 promptBuilder，新增 Reflections 段 |

---

## 7. Token 成本估算

> **成本控制策略**：不设 session token budget。结构参数（maxSteps/maxRetries/beamWidth）天然限制上限。
> API 层 spending limits 作为最终安全网。可选 `--max-tokens` flag 用于 CI 等成本敏感场景。

| 决策点 | 调用次数 | Token/调用 | 总计 |
|--------|---------|------------|------|
| Analyze (Scout) | 1-3 | 8K | 8-24K |
| Plan (Scout, 直接) | 1-2 | 4K | 4-8K |
| Plan (Scout, ToT) | 最多 3 轮 | 首轮 ~20K / 后续 ~60K | 20-140K |
| Steer (Agent, ReAct) | ~6 | 2K | 12K |
| Recover (Agent) | 0-6 | 2K | 0-12K |
| Judge Sonnet (初始) | 10-20 | 3K | 30-60K |
| Judge Haiku (批评) | ~50% × 20 | 1K | ~10K |
| Learn (Reflexion) | 1（批量） | 8K（含成功经验） | 8K |
| **典型 session（无 ToT）** | | | **~100K** |
| **典型 session（含 ToT 1-2 轮）** | | | **~160-220K** |

双模型批评额外约 10K（Haiku 便宜），批量反思 8K（含失败+成功）。ToT 通常 1-2 轮即收敛。

---

## 8. 实施分阶段计划

### C2a — Scout 头（无 agent 模式）
- 直接 AI 调用（Analyze + Plan），无迭代循环
- Prompt 模板：项目分析、测试规划
- 置信度驱动的测试排序

### C2b — Agent 头（ReAct 模式）
- `executor.go`：规则引擎 + ReAct 循环
  - 🔗 **JIT 参考**：`langgraph/pregel/_loop.py` — ReAct 循环的状态机实现（Thought→Action→Observation 转换）
  - 🔗 **JIT 参考**：`reflexion/hotpotqa_runs/react.py` — ReAct agent 的 prompt 结构和 output parsing
- `recovery.go`：失败恢复 + Skill 式反思注入
  - 🔗 **JIT 参考**：`reflexion/programming_runs/reflexion.py` — `self_reflection()` + 下次尝试注入反思的流程
- Steer/Recover prompt 模板
  - 🔗 **JIT 参考**：`reflexion/alfworld_runs/prompts/alfworld.json` — 反思 prompt 的措辞和格式
- LLM 输出解析兜底
  - 🔗 **JIT 参考**：`reflexion/hotpotqa_runs/react.py` — `parse_action()` 对 LLM 输出的容错处理

### C4+ — Agent 头（Sub-Agent 并行）
- `orchestrator.go`：DAG 构建 + 拓扑分层调度 + 结果收集
  - 🔗 **JIT 参考**：`langgraph/pregel/_executor.py` — `BackgroundExecutor` 并发调度模式（submit/done/wait/cancel）
  - 🔗 **JIT 参考**：`langgraph/pregel/_algo.py` — DAG 拓扑排序和分层执行算法
- `subagent.go`：SubAgent 封装（独立 ReAct + Driver）
  - 🔗 **JIT 参考**：`langgraph/pregel/_runner.py` — 单个 node 的执行上下文隔离
- `session_context.go`：SessionContext 共享状态 + TokenManager（线程安全）
  - 🔗 **JIT 参考**：`langgraph/channels/base.py` — 状态 channel 的读写隔离设计
- `max_concurrent_agents` 配置（默认 1 = 串行，渐进启用）
- 依赖分析：确定性规则推断 TestCase 间依赖（POST→GET→PUT→DELETE）
- 错误隔离：单 agent 失败 → skip，依赖级联，Orchestrator 全局 rate limit 暂停
  - 🔗 **JIT 参考**：`langgraph/pregel/_executor.py` — `__cancel_on_exit__` 和 `__reraise_on_exit__` 错误传播控制

### C3 — Examiner 头（Reflexion + Self-Refine 双模型 + Prompt 注册）
- `judge.go`：双模型判断（Sonnet 初始 + Haiku 批评）
  - 🔗 **JIT 参考**：`self-refine/src/acronym/run.py` — 三阶段循环（generate→feedback→refine）的实现模式
  - 🔗 **JIT 参考**：`self-refine/` — critique prompt 的措辞（"what could be improved" 而非 "is this wrong"）
- `learner.go`：双向反思批量生成（失败 + 成功经验）+ 质量门控 + effectiveness 管理 + 每类别上限 3 条后处理
  - 🔗 **JIT 参考**：`reflexion/programming_runs/reflexion.py` — `self_reflection()` 的调用时机和反思内容结构
  - 🔗 **JIT 参考**：`reflexion/alfworld_runs/generate_reflections.py` — 反思生成 prompt 和输出解析
- `strategy_matcher.go`：glob 匹配调度，失败 ≤2 条 + 成功 ≤1 条注入
- `policy.go`：Uncertain 3 级降级链（Self-Refine = Level 1 的实现）
- `examiner.go`：Checker-Judge 路由 + 裁决合并
  - 🔗 **JIT 参考**：`langgraph/graph/state.py` — 状态聚合模式（多来源结果合并策略）
- `internal/ai/prompts/`：Prompt 注册中心（embed.FS 默认 + overlay 覆盖）
  - 🔗 **JIT 参考**：`langgraph/pregel/_checkpoint.py` — 配置加载和回退机制
- 新增配置项：`settings.critic_model`（默认 `claude-haiku-4-5-20251001`，可覆盖）

### C4 — 记忆系统增强 + ToT 深度规划
- V002 migration：memory_procedural 添加 category、type、created_at 列
- `memory.go` 扩展：GetProceduralByEffectiveness + MarkStale（过期标记）
- 全局策略模板库（编译时嵌入，`cerberus init` 写入 L3）
- Effectiveness EMA 更新 + 归档 + 过期标记（90 天且 <0.5）
  - 🔗 **JIT 参考**：`reflexion/programming_runs/reflexion.py` — 反思的累积和重用逻辑（`reflections` list 的生命周期）
- SQLite WAL 模式启用（并发读安全）
- ToT beam search（`tot.go`）：AI 为主(70%) + 确定性为辅(30%)评估
  - 🔗 **JIT 参考**：`princeton-nlp/tree-of-thought-llm/src/tot/methods/bfs.py` — `solve()` 函数：generate→evaluate→select 三阶段循环
  - 🔗 **JIT 参考**：`princeton-nlp/tree-of-thought-llm/src/tot/methods/bfs.py` — `get_values()` 候选评分和 `get_proposals()` 候选生成
  - 🔗 **JIT 参考**：`princeton-nlp/tree-of-thought-llm/src/tot/models.py` — LLM 调用的批量并发模式
- `--deep-plan` CLI flag 触发，成本由结构参数控制（`maxSteps/beamWidth/generateN`）
- 可选 `--max-tokens` 作为成本安全网（CI 场景）

### C5-C6 — 基础设施（无新 agent 模式）

---

## 9. 验证方法

### C2b 验证（ReAct 执行循环）
1. 准备 HTTP 测试目标（httpbin.org）
2. 运行 `cerberus run --url https://httpbin.org --goal "测试 GET/POST"`
3. 验证：规则引擎 ≥ 70%，Steer 按需触发，Recovery ≤ 3 次

### C3 验证（Reflexion + Self-Refine 双模型 + Prompt 注册）
1. 构造 uncertain 场景
2. Self-Refine：confidence < 0.9 → Haiku 批评触发 → 修正 verdict
3. Reflexion：session 结束后 memory_procedural 有新反思 + 正确 category + type（failure/success）
4. 下次 run：glob 匹配到相关反思，失败 ≤2 条 + 成功 ≤1 条
5. 验证不相关反思（如 timeout 策略）未被注入到 auth 失败场景
6. 质量门控：构造空 diagnosis / 无效 pattern 的反思 → 验证被静默丢弃
7. Prompt 覆盖：在 `.cerberus/prompts/` 放置自定义 steer.yaml → 验证生效

### C4 验证（Effectiveness + ToT + 全局模板 + 过期）
1. 新项目首次 run → 自动继承全局策略模板
2. 运行 3 次 session，反思 effectiveness 正确更新
3. 无效反思 < 0.2 自动归档
4. `cerberus run --deep-plan`：ToT 覆盖端点数 > DirectPlanner
5. 验证 ToT 在 maxSteps 轮内正常完成或 API 错误时优雅返回
6. 可选：`--max-tokens 100000` 时 ToT 在 token 上限处停止
7. 过期验证：手动设置 created_at > 90 天 + effectiveness < 0.5 → 标记 stale → 不注入

### C4+ 验证（Sub-Agent 并行）
1. `max_concurrent_agents=1`：行为与 C2b 串行完全一致（退化为零开销）
2. `max_concurrent_agents=2`：2 个独立端点（GET /users + GET /posts）并行执行 → 耗时约串行 50%
3. 依赖验证：POST /users → GET /users/:id → 确保按序执行，不并行
4. 错误隔离：一个 sub-agent 超时 → 另一个正常完成，结果不丢失
5. 级联 skip：POST /users 失败 → 依赖它的 GET/PUT/DELETE 全部 skip
6. Auth 共享：一个 sub-agent 触发 token 刷新 → 其他 agent 立即获得新 token

---

## 10. 跨领域关注点

### 可观测性

每个决策点输出结构化 JSON trace（非 printf），供调试和 session 汇总使用：

```json
{"ts": "...", "phase": "steer", "attempt": 2, "action": "click", "target": "#submit", "latency_ms": 1200}
{"ts": "...", "phase": "judge", "model": "sonnet", "confidence": 0.85, "critique_triggered": true, "corrected": false}
{"ts": "...", "phase": "learn", "reflections_generated": 3, "types": ["failure","failure","success"], "quality_passed": 2}
```

Session 结束时输出汇总：

```
Session Summary:
  Verdicts: 20 pass, 3 fail, 2 uncertain
  Steer: 6 calls (2 fallback to ReAct)
  Self-Refine: 10 critiques triggered, 3 corrections
  Reflexion: 5 reflections stored (3 failure, 2 success)
  Strategy hits: 3/5 matched (2 failure, 1 success)
  Total tokens: ~105K
```

### 配置完整性

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `critic_model` | `claude-haiku-4-5-20251001` | Self-Refine 批评模型 |
| `max_critiques` | 1 | 每 verdict 最大批评次数 |
| `tot_beam_width` | 3 | ToT beam 宽度 |
| `tot_generate_n` | 5 | ToT 候选生成数 |
| `tot_max_steps` | 3 | ToT 最大步数 |
| `reflection_archive_threshold` | 0.2 | effectiveness 归档阈值 |
| `reflection_pin_threshold` | 0.7 | effectiveness pin 阈值 |
| `reflection_pin_min_uses` | 5 | pin 最少使用次数 |
| `reflection_expiry_days` | 90 | 反思过期天数 |
| `reflection_max_failure` | 2 | 每次注入最大失败反思数 |
| `reflection_max_success` | 1 | 每次注入最大成功经验数 |
| `max_concurrent_agents` | 1 | Sub-Agent 并发上限（1 = 串行） |

### 错误恢复统一策略

| 错误类型 | 恢复策略 | 影响范围 |
|----------|----------|----------|
| LLM API 错误（5xx） | 指数退避重试，最多 3 次 | ToT → break 返回当前最优；Judge → 返回初始判断 |
| Rate limit（429） | 退避重试，最多 3 次 | 同上 |
| 输出解析失败 | 兜底提取（ReAct）或返回初始判断（Judge） | 单次降级，不重试 |
| 网络超时 | 标记 skip，不触发反思 | 单个 verdict |
| Prompt 加载失败 | 回退 defaults（embed.FS） | 零配置始终可用 |

### 测试策略（Agent 模式自身）

- **单元测试**：Mock LLM 响应（预设 JSON）→ 验证 ReAct 循环流程、Self-Refine 早停/修正、Reflexion 质量门控
- **集成测试**：httpbin.org 作为固定目标，端到端跑完整 session
- **Prompt 回归**：PromptTemplate 版本变更时跑 golden test（固定输入 → 对比输出结构）

### 并发安全

- SQLite WAL 模式：允许并发读 + 单写，多 session CI 并行不阻塞
- L3 写入通过 `sync.Mutex` 或 channel-based writer 序列化
- effectiveness 更新为原子操作（read → compute → write 在锁内完成）
