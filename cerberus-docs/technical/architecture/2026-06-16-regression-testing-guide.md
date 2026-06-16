# 架构分析器 - 回归测试系统

## 概述

回归测试系统确保架构分析器在修复bug后不会重新引入同样的问题，并追踪分析器的准确率历史。

## 数据库架构

### 四个核心表

#### 1. `regression_tests` - 回归测试用例
存储每个已修复bug的回归测试，确保问题不会重现。

**字段:**
- `name`: 测试名称
- `bug_id`: 关联的bug ID (如 "BUG-001")
- `category`: 测试类别 (complexity/abstraction/solid)
- `test_type`: positive (应该检测到问题) 或 negative (不应该误报)
- `file_path`: 目标文件路径
- `expected_result`: 预期结果
- `status`: pending/pass/fail/skip

#### 2. `known_issues` - 已知问题和误报
记录架构分析中发现的真实问题和误报。

**字段:**
- `issue_type`: over_engineering 或 false_positive
- `file_path`: 文件路径
- `is_false_positive`: 是否为误报
- `verified_by`: 验证人 (manual_review 或 automated_analysis)
- `description`: 问题描述

#### 3. `accuracy_reports` - 准确率历史
追踪分析器在不同版本的准确率表现。

**字段:**
- `run_id`: 运行唯一标识符
- `total_issues`: 总问题数
- `true_positives`: 真实问题数
- `false_positives`: 误报数
- `overall_accuracy`: 总体准确率 (TP + TN) / Total
- `complexity_accuracy`: 复杂度检测准确率
- `abstraction_accuracy`: 抽象检测准确率
- `solid_accuracy`: SOLID原则检测准确率

#### 4. `bug_tracker` - Bug追踪
记录bug的生命周期管理。

**字段:**
- `bug_id`: bug唯一标识符
- `status`: open/in_progress/fixed/closed
- `category`: accuracy/performance/usability
- `root_cause`: 根本原因
- `regression_test_id`: 关联的回归测试ID

## CLI命令

### 1. 运行回归测试

```bash
# 运行所有回归测试
cerberus regression

# 运行特定类别的测试
cerberus regression --category complexity

# 详细输出
cerberus regression --verbose
```

**输出示例:**
```
运行 4 个回归测试...

[abstraction] BUG-001-Provider接口检测
  描述: 应该检测到TrigramProvider实现了Provider接口
  ✓ 通过

[complexity] 检测-initCmd高复杂度
  描述: 应该检测到initCmd函数的圈复杂度问题
  ✓ 通过

=====================================
总计: 4 通过, 0 失败, 0 跳过
```

### 2. 查看已知问题

```bash
# 列出所有已知问题
cerberus known-issue list
```

**输出示例:**
```
已知问题:
=====================================
⊘ 误报 [false_positive] internal/embed/provider.go:0
  Provider接口被报告为未使用，但TrigramProvider实现了它
  验证者: manual_review @ 2026-06-16

✓ 真实问题 [true_positive] cmd/cerberus/main.go:62
  initCmd函数圈复杂度14，超过阈值10
  验证者: automated_analysis @ 2026-06-16
```

### 3. 查看准确率报告

```bash
# 查看准确率历史（默认最近10条）
cerberus accuracy

# 查看更多历史记录
cerberus accuracy --limit 20
```

**输出示例:**
```
准确率历史:
=====================================
运行 ID: BASELINE-2026-06-16
时间: 2026-06-16 00:00:00
项目: /home/mason/Documents/code_projects/private/cerberus
总问题: 4 | 真阳性: 2 | 假阳性: 1 | 真阴性: 0
总体准确率: 50.0%
  复杂度: 100.0%
  抽象: 0.0%
-------------------------------------
```

## 工作流程

### 1. 发现bug或误报时

当架构分析器报告问题时：

```bash
# 1. 手工验证
cerberus architecture
# 查看报告，确认是否为真实问题

# 2. 如果是误报，记录到数据库
# (目前需要手动插入 known_issues 表)

# 3. 修复代码
vim internal/architecture/abstraction.go
```

### 2. 创建回归测试

修复bug后，创建回归测试防止复发：

```bash
# 插入 regression_tests 表
# - test_type = 'positive' 表示应该检测到问题
# - test_type = 'negative' 表示不应该误报

# 例子: BUG-001的Provider接口检测
INSERT INTO regression_tests (
    name, bug_id, category, test_type,
    description, file_path, expected_result
) VALUES (
    'BUG-001-Provider接口检测',
    'BUG-001',
    'abstraction',
    'positive',
    '应该检测到TrigramProvider实现了Provider接口',
    'internal/embed/trigram.go',
    'detected_implementation'
);
```

### 3. 定期验证

```bash
# 每次修改架构分析代码后，运行回归测试
cerberus regression

# 如果测试失败，说明修改引入了回归问题
# 需要修复代码后再运行
```

### 4. 追踪准确率

```bash
# 定期查看准确率趋势
cerberus accuracy --limit 50

# 观察各类别的准确率变化
# - 复杂度检测准确率
# - 抽象检测准确率
# - SOLID原则检测准确率
```

## 初始数据

系统已预装以下测试数据：

### 回归测试 (4条)
1. BUG-001-Provider接口检测 (抽象)
2. BUG-001-不存在接口不应检测 (抽象)
3. 检测-initCmd高复杂度 (复杂度)
4. 检测main.go文件过长 (复杂度)

### 已知问题 (3条)
1. **误报**: Provider接口被报告为未使用 (BUG-001)
2. **真实问题**: initCmd函数圈复杂度14
3. **真实问题**: main.go文件507行

### Bug记录 (1条)
1. BUG-001 - Provider接口检测不准确

### 准确率基线 (1条)
- 总体准确率: 50% (2/4正确)
- 复杂度: 100% (2/2正确)
- 抽象: 0% (0/1正确，有1个误报)

## 准确率计算

### 指标定义

- **True Positive (TP)**: 真实问题，正确检测
- **False Positive (FP)**: 正常代码，误报为问题
- **True Negative (TN)**: 正常代码，正确不报
- **False Negative (FN)**: 真实问题，漏报

### 准确率公式

```
Overall Accuracy = (TP + TN) / Total
Category Accuracy = TP_category / (TP_category + FP_category)
```

### 当前基线

- **复杂度检测**: 100% (2/2)
  - ✓ 检测到initCmd高复杂度
  - ✓ 检测到main.go过长
  
- **抽象检测**: 0% (0/1)
  - ✗ Provider接口误报为未使用

## 扩展指南

### 添加新的回归测试

当发现新的bug或修复现有bug时：

1. **记录问题到 `known_issues` 表**
2. **创建对应的回归测试**
3. **运行测试验证修复**
4. **更新准确率基线**

### 添加新的准确率报告

运行架构分析后：

```sql
INSERT INTO accuracy_reports (
    run_id, project_path,
    total_issues, true_positives, false_positives, true_negatives,
    overall_accuracy, complexity_accuracy, abstraction_accuracy
) VALUES (
    'RUN-2026-06-16-001',
    '/path/to/project',
    5, 4, 1, 0,
    0.80, 1.0, 0.5
);
```

## 故障排查

### 测试失败

如果回归测试失败：

1. 检查架构分析器是否正确运行
2. 验证测试用例的 `expected_result` 是否正确
3. 使用 `--verbose` 查看详细输出
4. 检查是否引入了新的bug

### 数据库问题

如果遇到数据库错误：

1. 确保migration已运行: `cerberus init`
2. 检查表是否存在: 查看数据库中的表列表
3. 如果数据缺失，手动运行种子数据SQL

## 下一步

- [ ] 实现 `cerberus known-issue add` 交互式命令
- [ ] 自动从已知问题生成回归测试
- [ ] 集成到CI/CD流程
- [ ] 生成准确率趋势图表
- [ ] 添加回归测试覆盖率报告
