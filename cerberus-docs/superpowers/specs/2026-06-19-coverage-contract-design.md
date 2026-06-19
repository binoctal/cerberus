# Coverage Contract(覆盖契约)设计

- 日期:2026-06-19
- 状态:design(待 review)
- 关联:cerberus self-test 改进(7 轮 dogfooding 后的覆盖方法论升级)

## 概述

为 cerberus 引入**覆盖契约(CoverageContract)**:一个由 AI 事前定义、贯穿 run 全程的测试覆盖标准。它回答"该测什么、怎么测、测多深",作为 Scout 规划与 Examiner 评估的共同依据,补上 cerberus 当前缺失的"事前覆盖标准"层。

设计原则:**AI 主导语义判断(该测什么 / 测够了吗)+ 工具辅助客观验证(覆盖率)**,延续 cerberus AutoTest 已验证的 `cover + LLM` 结合模式。

## 背景与问题

cerberus 现有流程:`Scout(Analyze + Plan 生成 case) → Agent(执行) → Examiner(判单 case)`。

暴露的缺口(7 轮 dogfooding 验证):

1. **Scout 直接跳到"生成 case",跳过了"先定义该测什么、测多深"**——case 是具体测试,缺上层"元标准"。
2. **没有事前标准,Examiner 无法判断"整体测够了没"**——只能判单 case 对错,无法判 session 级充分性(没有对照)。
3. **覆盖判断不可靠地依赖 LLM 主观**——没有客观数据(覆盖率)兜底,易误判(第四轮前 Examiner 误判 framework error 为 pass)。

## 设计目标

- **事前定义**:Scout 在生成 case 前,先产出覆盖契约(范围/维度/深度/优先级/不变量)。
- **贯穿全程**:契约同时指导 case 生成(Agent 执行)和 session 级评估(Examiner)。
- **三层评判**:(a) 契约合理性(事前 meta 自评)、(b) 充分性标准(定义在契约)、(c) 达成评估(事后对照)。
- **AI + 工具结合**:AI 定语义(契约 + 评估),工具(coverage)验证客观达成。
- **跨项目通用**:Go / Node / Python / SaaS 项目都能用。

## 非目标(YAGNI)

- 不新增第 4 个 head——作为 Scout/Examiner 的增强,避免架构膨胀。
- 不引入独立 `cerberus audit` 命令作为核心——契约评估是 `run` 的自动一步(命令仅作复用便利,后续再加)。
- 不把 regression(已知 bug 不再犯)纳入契约——regression 是正交关注,属 `cerberus verify/accuracy` 模式。
- 不覆盖 OpenAI/Gemini provider 端到端(本次设计 provider-agnostic,provider 验证另议)。

## 核心设计

### 1. CoverageContract 数据模型

```
CoverageContract {
  Depth       string            // smoke | standard | thorough(档位)
  Scope       []string          // 测哪些模块/路径(从 goal + 项目结构推)
  PathTypes   []string          // happy | alternative | boundary | edge(档位展开)
  ErrorScope  []string          // none | 4xx | validation | exception(档位展开)
  Boundaries  []string          // empty | zero | max | invalid | extreme(档位展开)
  Invariants  []InvariantRef    // 从 project.yaml invariants 带入,档位决定校验严度
  Priorities  map[string]string // 模块/功能 → high|med|low;风险权重,指导 case 生成顺序 + Examiner 缺口排序
  CoverageGate CoverageGate     // 客观门禁(模块 + 阈值)
}

CoverageGate { Module string; LineThreshold float64; BranchThreshold float64 }
// 语言无关:各 coverage provider(Go/Node/Python)各自度量,契约只存阈值。
```

契约由 Scout 产出,持久化到 session(供 Examiner 对照 + resume)。

### 2. 深度档位

档位是"维度的预设组合"。用户在 `project.yaml` 声明一行,AI 在档位约束下细化。

| 维度 | smoke | standard(默认) | thorough |
|------|-------|----------|----------|
| 路径类型 | happy | + alternative | + boundary + edge |
| 功能广度 | 关键入口/主流程 | 全部公开接口 | + 内部关键逻辑 |
| 错误处理 | 仅"通/不通" | 主要错误(4xx、校验) | + 异常(panic/超时/资源) |
| 边界值 | 否 | 是(空/零/最大/非法) | + 极端/压力 |
| 不变量 | 核心硬约束 | 全部 | 强校验(多数据组合) |
| 并发安全 | 否 | 否 | 是 |
| 性能 | 否 | 否 | 基本(时延/吞吐) |
| 覆盖率门禁 | 无 | 关键模块 ~60-70% | 分支 ~85%+ |
| 预期 case 数 | 5-15 | 15-40 | 40+ |
| 适用场景 | CI 每次提交 | 合并/常规发版 | 重大发版/安全敏感 |

档位只控"测多深",不控"测什么语言/executor"——后者由项目本身决定(Go→process_exec+autotest,SaaS→http),档位跨语言通用。

阈值是**建议默认值**,可在 `project.yaml` 项目级覆盖(`coverage.line_threshold`)而不改档位语义。

### 3. 流程

```
project.yaml: coverage.depth = standard
        │
        ▼
Scout(事前):
  1. AI 分析项目 + goal,产出 CoverageContract(范围/维度/档位展开/优先级/invariants/门禁)
  2. AI 自评契约(meta):范围漏了吗?维度全吗?→ 修正
  3. 据契约生成 case(现有 Plan 增强:每个 case 关联契约维度)
        │
        ▼
Agent(执行):按 case 执行(契约已蕴含在 case 里)
        │
        ▼
Examiner(事后):
  1. 单 case judge(现有,不变)
  2. session 级对照契约评估(新增):
     - 范围:契约 Scope 都有 case 覆盖吗?漏哪个?
     - 深度:PathTypes/ErrorScope/Boundaries 都有 case 体现吗?
     - 客观:coverage 报告对照 CoverageGate,达标吗?
     - 产出:CoverageAssessment{ reached: bool, gaps: [], coverage_pct }
```

## AI + 工具分工

| 任务 | 主导 | 说明 |
|------|------|------|
| 定覆盖目标(范围/what/深度) | AI | 语义判断,工具做不到 |
| 契约自评(合理性) | AI | meta 判断 |
| 生成 case | AI(现有 Scout) | 朝契约维度生成 |
| 单 case 判断 | AI(现有 Examiner) | |
| session 级达成评估(语义) | AI | 对照契约,判"够不够" |
| 覆盖率度量(客观) | 工具 | go test -cover / cerberus coverage report |
| 覆盖率门禁(硬阈值) | 工具 | CoverageGate 客观判定 |
| 确定性边界(整数溢出/并发/协议) | 工具(fuzz/property) | AI 不擅长机械穷尽 |

## 组件改动

| 组件 | 改动 | 说明 |
|------|------|------|
| `internal/head/scout/` | 新增 `contract.go` + Scout.Plan 前置 `BuildCoverageContract` | AI 产出契约 + 自评 |
| `internal/head/examiner/` | 新增 `assess.go` + Examiner 加 `AssessCoverage` | session 级对照契约评估 |
| `internal/project/schema.go` | `Settings` 加 `Coverage CoverageSettings` | 档位配置 |
| `internal/session/` | `run_phases_scout` 产出契约存 session;`run_phases_examiner` 末尾调 AssessCoverage | 串接 |
| `internal/store/` | 契约 + 评估持久化 | 供 resume / report |
| `cmd/cerberus/report` | 展示契约 + 评估 + 缺口 | 用户可见 |

`CoverageContract` / `CoverageAssessment` 类型放新建 `internal/head/contract/` 子包(scout 产、examiner 消费,避免两个 head 包互相 import)。

## 配置

```yaml
settings:
  coverage:
    depth: standard              # smoke | standard | thorough
    line_threshold: 0.65         # 可选,覆盖档位默认阈值(关键模块)
    branch_threshold: 0.50       # 可选
```

未配置时默认 `standard`。向后兼容:现有 project.yaml 无 `coverage` 块也能跑(走 standard 默认)。

## 成本与降级

- 契约 + 自评 = 每 run 多 ~2 次 LLM 调用(产契约 + meta 自评)。
- smoke 档位可跳过 meta 自评(快速 CI 门,降为 +1 调用)。
- 契约缓存(同项目+goal+档位复用)为后续优化,**初版 YAGNI 不做**。

## fixture 验证矩阵

契约能力本身要跨项目验证。建 `test/fixtures/`:

```
test/fixtures/
├── go-lib/        # 最小 Go 库 + 未覆盖函数
├── node-app/      # 最小 Node + jest
├── python-pkg/    # 最小 Python + pytest
└── saas-api/      # 最小 HTTP server
```

每个 fixture 配集成测试:`cerberus run --dir <fixture>` + **mock LLM**(确定性、不烧钱、进 CI),验证:
- Scout 对该语言/结构能产出合理 CoverageContract。
- 档位展开对该 project-type 合理(Go→process/autotest,SaaS→http)。
- Examiner 的 AssessCoverage 能对照契约给合理评估 + 缺口。
- AutoTest 的 coverage 解析对 Node/Python 真能用(当前 coverage_node_parse / coverage_python_parse 从未真实跑过)。

## 测试策略

- **单元**:CoverageContract 构建、档位展开、AssessCoverage 对照逻辑(纯函数,mock)。
- **集成**:fixture 矩阵 × 档位(mock LLM)。
- **回归**:把 7 轮 dogfooding 发现的 LLM 作妖模式(空 envelope、framework error)固化成 mock,确保契约/评估对这些鲁棒。
- **CI**:`make check` + `cerberus selftest` + fixture 矩阵。
- **真实 LLM 采样**:定期(非每次)用 GLM 跑 fixture,探未知模式 → 喂回 mock。

## 开放决策(已定)

1. **standard 含边界值**——是(边界是标准质量的一部分,非仅 thorough)。
2. **覆盖率阈值保留硬值**(60-70% / 85%)作为客观门禁,可项目级覆盖——而非完全交 AI 主观判(AI 自评不客观)。
3. **regression 不进档位**——正交关注,属 verify 模式。
4. **不新增 head**——作为 Scout/Examiner 增强(YAGNI)。
5. **契约产生:档位 + AI 细化**(混合)——用户一行控深度,AI 填细节。

## 后续(brainstorm 之后)

本 spec 批准后,进入 writing-plans 出实现计划,分阶段:
1. CoverageContract 类型 + 档位定义(纯数据,无 LLM)。
2. Scout.BuildCoverageContract + 自评(AI)。
3. Examiner.AssessCoverage(AI + 工具)。
4. 配置 + session 串接 + report。
5. fixture 矩阵(Go 起步 → Node → Python → SaaS)。
