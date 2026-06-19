# Fixture 矩阵设计(Plan 2)

- 日期:2026-06-20
- 状态:design(待 review)
- 关联:Plan 1(Coverage Contract,已实现 merged to main);cerberus 通用性验证

## 概述

验证 cerberus 作为通用测试框架,能测 4 类语言的外部项目(Go / Node / Python / SaaS)。用 fixture(最小样例项目)+ mock LLM(LLM 响应点)+ 真实 executor/autotest,验证 cerberus 对每语言的适配代码。

设计原则:**mock 只在"LLM 说什么"介入,cerberus 的执行/解析/判断代码全真实**——所以验的是 cerberus 代码的通用性,不是 LLM 质量。

## 背景与问题

前 7 轮 dogfood 只验了 **cerberus 测自己**(Go + 熟悉 + GLM 已知)——这是最不代表通用性的场景。cerberus 设计是测**任意项目**。

关键盲区(从未真实跑过的代码):
- `coverage_node_parse.go` / `coverage_node_gaps.go` / `gen_node_extract.go` — Node autotest 链路,从没跑过真实 Node 项目。
- `coverage_python_parse.go` / `coverage_python_gaps.go` / `gen_python_extract.go` — Python autotest 链路,从没跑过真实 Python 项目。
- HTTP executor 对真实 SaaS server 的端到端(之前只用 httptest mock 验证 API server 本身,没验 cerberus 测 SaaS)。

## 目标

- 验证 cerberus 对 Go/Node/Python/SaaS fixture 的 Scout→Agent→Examiner→AutoTest 流程跑通。
- 真实验证 executor 对各语言的分派(Go:process_exec `go test`;Node:`npm test`;Python:`pytest`;SaaS:HTTP executor)。
- 真实验证 autotest coverage 解析 + test generator 对 Node/Python(**从没跑过的代码**)。
- 发现并修复跨语言 bug(TDD,像 AutoTest source 那次)。
- 全部进 CI(mock LLM,确定性,不烧钱)。

## 非目标(YAGNI)

- AI 质量验证(plan/judge 合理、生成的测试有用)——那是 LLM 能力,Plan 2 用 mock(验 cerberus 代码,非 LLM 质量)。AI 质量靠定期真实 dogfood(另议)。
- 真实 LLM 端到端——非确定 + 烧钱 + CI 难,另议。
- Plan 1(覆盖契约核心)——已实现 merged to main。
- Rust/Java/其他语言——fixture 矩阵可扩展,初版 4 类。

## 设计

### fixture(test/fixtures/)

4 个最小但真实的项目(有可测代码 + 测试工具):

```
test/fixtures/
├── go-lib/
│   ├── go.mod              # module github.com/binoctal/cerberus/test/fixtures/go-lib
│   └── math.go             # func Add(a,b int) int  ← 未覆盖(无 math_test.go)
├── node-app/
│   ├── package.json        # "test": "jest", devDeps: jest
│   ├── lib.js              # function add(a,b){return a+b}  ← 未覆盖(无 lib.test.js)
│   └── jest.config.js      # coverageReporters: ["json"]
├── python-pkg/
│   ├── requirements.txt    # pytest, pytest-cov
│   └── math.py             # def add(a,b): return a+b  ← 未覆盖(无 test_math.py)
├── saas-api/
│   └── server.go           # httptest server: GET /health→200, GET /users→200
└── mock_helper_test.go     # 共享 mock LLM 响应集(见下)
```

每 fixture 有真实可测代码(函数 + 未覆盖)+ 真实测试工具链(go test / jest / pytest)。

### mock 策略(核心)

**mock 只在 LLM 调用点介入**,cerberus 的执行/解析/判断全真实:

| 步骤 | 真实 / mock | 验什么 |
|------|------------|--------|
| Scout.Plan(LLM 生成 case) | **mock** | — |
| BuildCoverageContract(LLM 生成契约) | **mock** | — |
| Agent executor(Go:go test / Node:npm test / Python:pytest / SaaS:HTTP) | **真实** | executor 对各语言分派对不对 |
| AutoTest coverage(go cover / jest --coverage / pytest --cov) | **真实** | coverage_go/node/python_parse 真能解析 |
| AutoTest generator 提取源码函数 | **真实** | gen_go/node/python_extract 真能提取 |
| AutoTest generator LLM 写测试 | **mock** | — |
| Examiner.Judge(LLM 判断) | **mock** | — |
| AssessCoverage 客观门禁比较 | **真实** | 纯代码,mock 不碰 |

### 共享 mock helper

`test/fixtures/mock_helper_test.go`:统一 mock 响应集,让所有 fixture 集成测试复用:
- Plan JSON:`{"cases":[{"id":"tc-1","target":"<fixture 文件>","action":"file_read"}]}`
- Contract JSON:`{"depth":"standard","scope":["<fixture 文件>"],"coverage_gate":{"line_threshold":0.5}}`
- generator JSON:`{"test":"...预设测试代码..."}`

fixture 间的差异(语言/文件名)通过参数注入。

### 集成测试(每 fixture 一个)

`test/fixtures/<lang>_fixture_test.go`:
1. 构造 mock LLM(共享 helper)+ cerberus session(指向 fixture 目录)。
2. `session.Run(ctx)`(或直接调 Scout/Agent/Examiner/AutoTest phase)。
3. 断言(见下)。

### 断言(每语言)

- **Go**:go test 真跑通 + coverage 解析提取 Add 未覆盖 + autotest 生成 Go 测试(based on Add 源码)。
- **Node**:npm test 真跑通 + **jest coverage 解析(coverage_node_parse,从没跑过!)** + autotest 生成 Node 测试(gen_node_extract 提取 add)。
- **Python**:pytest 真跑通 + **coverage 解析(coverage_python_parse,从没跑过!)** + autotest 生成 Python 测试。
- **SaaS**:HTTP executor 真发请求 + server 真响应 + verdicts 产出 + Contract 评估。
- **通用**:session completed + Contract != nil + verdicts 产出。

### bug 处理

Node/Python autotest 暴露的 bug(coverage 解析格式不匹配 / generator 提取失败 / executor 调用错)→ 当场 TDD 修(像 AutoTest source_path/extractFunc 那次)。这是 Plan 2 的核心价值——**发现并修复跨语言 bug**。

## 组件

| 组件 | 职责 |
|------|------|
| `test/fixtures/go-lib/` | Go fixture 项目 |
| `test/fixtures/node-app/` | Node fixture 项目 |
| `test/fixtures/python-pkg/` | Python fixture 项目 |
| `test/fixtures/saas-api/` | SaaS fixture(httptest server) |
| `test/fixtures/mock_helper_test.go` | 共享 mock LLM 响应 |
| `test/fixtures/go_lib_fixture_test.go` | Go 集成测试 |
| `test/fixtures/node_app_fixture_test.go` | Node 集成测试 |
| `test/fixtures/python_pkg_fixture_test.go` | Python 集成测试 |
| `test/fixtures/saas_api_fixture_test.go` | SaaS 集成测试 |

## 工具链与 CI

- Go fixture:`go test`(cerberus CI 已有 Go)。
- Node fixture:需 `node` + `npm` + `jest`。CI 需装或 **runtime 检测 + skip if 缺**(build tag `//go:build node_integration` 或 `exec.LookPath("npm")` 检测)。
- Python fixture:需 `python3` + `pytest` + `pytest-cov`。同上 skip if 缺。
- SaaS fixture:Go httptest(cerberus CI 已有 Go,无外部依赖)。

**CI 策略**:Go + SaaS fixture 每次 CI 跑(无外部依赖)。Node/Python fixture 在有工具链时跑(skip if 缺,不强制 CI 装)。长期可在 Docker CI 镜像含全工具链。

## 开放决策(已定)

1. **fixture 位置**:test/fixtures/(独立,清晰,不在 internal/)。
2. **mock helper**:共享(DRY,语言差异参数化)。
3. **CI 工具链**:检测 + skip if 缺(不强制 CI 装 Node/Python,但鼓励)。
4. **scope**:全 4 语言(Go/Node/Python/SaaS),用户确认。
5. **mock 策略**:mock LLM + 真实 executor/autotest(验 cerberus 代码,非 LLM 质量)。

## 后续(brainstorm 之后)

本 spec 批准后,进入 writing-plans 出实现计划,分阶段:
1. 共享 mock helper + Go fixture(起步,验证框架)。
2. SaaS fixture(验证跨模式)。
3. Node fixture(验证 autotest Node,最可能爆 bug)。
4. Python fixture(验证 autotest Python)。
5. bug 修复(如果 Node/Python 暴露)。
