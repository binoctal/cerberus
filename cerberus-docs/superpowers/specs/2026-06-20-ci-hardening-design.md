# CI 加固设计

- 日期:2026-06-20
- 状态:design(待 review)
- 关联:Plan 1(Coverage Contract)+ Plan 2(fixture 矩阵)

## 概述

3 个 CI 加固改进,解决 cerberus 当前 3 个诚实不足:
1. **fixture 工具链**:CI 装 Node/Python,跑全 4 fixture(不 skip)。
2. **真实行覆盖率**:AssessCoverage 接真实行覆盖率(非 pass-ratio 代理)。
3. **真实 LLM weekly schedule**:定期用真实 LLM 验 AI 质量 + 探未知模式。

设计原则:**不硬编码模型名/端点/key**——全部跟随环境变量(ANTHROPIC_DEFAULT_SONNET_MODEL / ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN),同 cerberus config 系统设计。

## 背景与不足

| 不足 | 现状 | 影响 |
|------|------|------|
| fixture CI 工具链 | Node/Python fixture skip if npm/python3 缺;CI 不装 → skip | CI 没验 Node/Python autotest(Plan 2 价值没兑现) |
| coverage 代理 | covPct = passed/total(pass-ratio),与 LineThreshold(行覆盖率)比较 | 单位不匹配,门禁"名义"非真客观 |
| 真实 LLM 手动 | CI mock LLM(确定性);真实 LLM 质量手动 dogfood | AI 质量 + 未知模式不持续验证 |

## 目标

- CI 每次跑全 4 fixture(Go+Node+Python+SaaS),不 skip。
- AssessCoverage 用真实行覆盖率(go/jest/pytest cover),非 pass-ratio。
- CI weekly 用真实 LLM 跑 fixture,验 AI 质量 + 探未知。
- 全部跟随环境变量,不硬编码模型/端点/key。

## 非目标(YAGNI)

- 不替换 mock CI(mock 仍每次跑,确定性,验 cerberus 代码;weekly 真实补充)。
- 不 daily schedule(weekly 足够——AI 质量变化慢于代码)。
- 不快照测试(快照过时 + 不探未知)。
- 不写死模型名(用 ANTHROPIC_DEFAULT_SONNET_MODEL 环境变量)。

## 设计

### #1 fixture CI 工具链

`.github/workflows/ci.yml` test job 加 Node + Python + fixture deps install:

```yaml
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
      - uses: actions/setup-python@v5
        with:
          python-version: '3.11'
      - name: Install fixture deps
        run: |
          cd test/fixtures/node-app && npm install --silent
          cd ../python-pkg && pip install -q -r requirements.txt
```

效果:CI 跑 `go test ./test/fixtures/` 时,Node/Python fixture 不 skip(有 npm+python+jest+pytest),全 4 fixture 验证。

### #2 真实行覆盖率(B 独立 step + A 复用)

**方案 B(主)+ A(优化)**:

Examiner 阶段(`run_phases_examiner.go`),AssessCoverage 前获取真实行覆盖率:
- **if `sess.LastAutoTestReport` 有 coverage**(AutoTest on):复用 `BeforeCoveragePct`(真实行覆盖率,AutoTest 已跑)。
- **else**(AutoTest off,默认):独立跑 coverage provider(go test -cover / jest --cov / pytest --cov),拿真实行覆盖率。
- 传真实 coveragePct 给 `AssessCoverage`。

复用 Plan 2 的 coverage provider(GoCoverageProvider.RunCoverage / Node / Python,语言路由已实现)。不新写 coverage 逻辑。

**数据流**:
```
Examiner 阶段:
  coveragePct = 0
  if sess.LastAutoTestReport != nil && HasCoverage(sess.LastAutoTestReport):
    coveragePct = sess.LastAutoTestReport.BeforeCoveragePct   // A: 复用 AutoTest
  else:
    report = coverageProvider.RunCoverage(ctx, projectDir)     // B: 独立跑
    coveragePct = report.CoveragePct
  AssessCoverage(ctx, contract, results, coveragePct)          // 真实行覆盖率
```

效果:AssessCoverage 的客观门禁(coveragePct < LineThreshold)用**真实行覆盖率**,非 pass-ratio。Coverage Contract 设计目的兑现。

### #3 真实 LLM weekly schedule

`.github/workflows/dogfood.yml`(新文件):

```yaml
name: Dogfood (real LLM)
on:
  schedule:
    - cron: '17 6 * * 1'   # weekly Monday ~6am UTC (off-peak)
  workflow_dispatch: {}     # manual trigger

jobs:
  dogfood:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - uses: actions/setup-python@v5
        with: { python-version: '3.11' }
      - name: Install fixture deps
        run: |
          cd test/fixtures/node-app && npm install --silent
          cd ../python-pkg && pip install -q -r requirements.txt
      - name: Build cerberus
        run: make build
      - name: Run dogfood (all fixtures, real LLM)
        env:
          ANTHROPIC_AUTH_TOKEN: ${{ secrets.ANTHROPIC_AUTH_TOKEN }}
          ANTHROPIC_BASE_URL: ${{ secrets.ANTHROPIC_BASE_URL }}
          ANTHROPIC_DEFAULT_SONNET_MODEL: ${{ secrets.ANTHROPIC_DEFAULT_SONNET_MODEL }}
        run: |
          for fixture in go-lib node-app python-pkg; do
            echo "=== dogfood $fixture ==="
            ./build/cerberus run --dir test/fixtures/$fixture --goal "dogfood $fixture" || echo "$fixture failed (review needed)"
          done
```

**关键设计:跟随环境变量,不写死模型**:
- `ANTHROPIC_DEFAULT_SONNET_MODEL`(from secrets)——cerberus config resolveModel 自动解析。换模型只改 secret,不改 CI。
- `ANTHROPIC_BASE_URL`(from secrets)——端点(GLM/Anthropic/其他)跟随。
- `ANTHROPIC_AUTH_TOKEN`(from secrets)——key。

效果:weekly 真实 LLM 跑 3 fixture(Go+Node+Python),验 AI 质量(plan/judge/生成)+ 探未知模式(GLM 作妖 → 喂回 mock)。失败不 block(|| echo,人工 review issue)。

**secrets 设置**(用户在 GitHub repo Settings → Secrets):
- `ANTHROPIC_AUTH_TOKEN`:LLM key。
- `ANTHROPIC_BASE_URL`:端点(如 https://open.bigmodel.cn/api/anthropic)。
- `ANTHROPIC_DEFAULT_SONNET_MODEL`:模型(如 GLM-4.7)。

## 组件

| 组件 | 改动 | 职责 |
|------|------|------|
| `.github/workflows/ci.yml` | 修改:加 setup-node/python + install | #1 fixture 工具链 |
| `internal/session/run_phases_examiner.go` | 修改:coverage step(复用 AutoTest 或独立跑) | #2 真实行覆盖率 |
| `internal/session/coverage.go`(或 examiner 辅助) | 新增:coverage 获取 helper | #2 |
| `.github/workflows/dogfood.yml` | 新增:weekly schedule + env secrets | #3 真实 LLM |

## 测试策略

- **#1**:CI 跑全 4 fixture(验证:不 skip,Node/Python autotest 真验)。本地 `make check` 仍绿(skip if 缺)。
- **#2**:单元测 coverage 获取 helper(AutoTest 有/无 → 复用/独立);集成测 AssessCoverage 拿真实行覆盖率。
- **#3**:workflow_dispatch 手动触发验证(跑一次真实 LLM);weekly 自动。

## 开放决策(已定)

1. **coverage 接入**:B(独立 step)+ A(AutoTest 复用优化)。
2. **LLM CI**:A weekly schedule(真实 LLM,非 daily/快照)。
3. **模型/端点/key**:全部 ANTHROPIC_* 环境变量(from GitHub secrets),不写死。
4. **失败处理**:dogfood 失败不 block CI(|| echo),人工 review issue。

## 后续(brainstorm 之后)

本 spec 批准后,writing-plans 出实现计划:
1. #1 CI 工具链(yaml 改,最简)。
2. #2 coverage step(session 改,中)。
3. #3 dogfood.yml(新 workflow,中)。
