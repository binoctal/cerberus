# 运行时文件管理重构实施计划

**目标**: 建立标准的运行时文件管理机制，区分开发时和部署后的文件位置

**日期**: 2026-06-16

## 架构设计

### 开发时（从源码运行）
```
cerberus/
├── build/          # 构建产物（gitignore）
│   └── cerberus
├── runtime/        # 运行时文件（gitignore）
│   ├── data/
│   ├── logs/
│   └── cache/
└── .cerberus/      # 配置（gitignore 部分文件）
```

### 部署后（用户安装）
- **Linux**: `~/.config/cerberus/` (配置), `~/.local/share/cerberus/` (数据), `~/.local/state/cerberus/` (日志), `~/.cache/cerberus/` (缓存)
- **macOS**: 同 Linux
- **Windows**: `%APPDATA%\Cerberus\` (配置), `%LOCALAPPDATA%\Cerberus\` (数据/日志/缓存)
- **Docker**: `/app/config/`, `/app/data/`, `/app/logs/`, `/app/cache/`

## 实施任务

### Phase 1: 核心路径管理（新建包）

#### Task 1.1: 创建 runtime/paths.go
**目标**: 实现运行时路径管理核心逻辑

**文件**: `internal/runtime/paths.go`

**实现内容**:
- `Paths` 结构体：ConfigDir, DataDir, LogsDir, CacheDir, DBPath
- `New() *Paths`: 自动检测环境并返回对应路径
- `newLinuxPaths()`: Linux XDG 标准路径
- `newMacOSPaths()`: macOS 标准路径
- `newWindowsPaths()`: Windows 标准路径
- `newDockerPaths()`: Docker 容器路径
- `newDevelopmentPaths()`: 开发环境路径
- `isInDocker()`: Docker 环境检测
- `Ensure()`: 确保所有目录存在
- `getEnv()`: 环境变量读取（带降级）

**测试**:
- 测试 Linux 路径生成
- 测试 macOS 路径生成
- 测试 Windows 路径生成
- 测试 Docker 路径生成
- 测试开发环境路径
- 测试目录创建

**验收标准**:
- 所有函数覆盖 4 个操作系统 + Docker + 开发环境
- 路径符合各平台标准
- Ensure() 正确创建所有目录

---

#### Task 1.2: 创建 runtime/detect.go
**目标**: 实现开发环境自动检测

**文件**: `internal/runtime/detect.go`

**实现内容**:
- `IsDevelopment() bool`: 检查是否在 cerberus 项目目录运行
  - 检查 `go.mod` 存在
  - 检查 `go.mod` 包含 `module github.com/binoctal/cerberus`
- `GetPaths() *Paths`: 根据环境返回合适的路径
- `contains(s, substr string) bool`: 字符串包含检查

**测试**:
- 测试在 cerberus 项目目录检测
- 测试在非项目目录检测
- 测试 go.mod 不存在情况
- 测试 go.mod 模块名不匹配情况

**验收标准**:
- IsDevelopment() 正确识别 cerberus 项目
- GetPaths() 根据环境返回正确路径

---

### Phase 2: Config 集成

#### Task 2.1: 更新 config/config.go
**目标**: 集成运行时路径到配置系统

**文件**: `internal/config/config.go`

**实现内容**:
- 添加 `Paths *runtime.Paths` 字段到 `Config` 结构体
- 在 `Load()` 函数中：
  - 调用 `runtime.GetPaths()` 获取路径
  - 调用 `paths.Ensure()` 确保目录存在
  - 使用 `paths.DBPath` 作为默认数据库路径
- 更新默认 DBPath 逻辑从 `cerberus.db` 改为 `paths.DBPath`

**测试**:
- 测试配置加载包含 Paths
- 测试 DBPath 使用运行时路径
- 测试环境变量 CERBERUS_DB_PATH 仍然有效

**验收标准**:
- Config.Paths 正确初始化
- DBPath 默认指向运行时目录
- 环境变量覆盖仍然工作

---

#### Task 2.2: 更新 config 测试
**目标**: 修复测试以适应新的默认路径

**文件**: `internal/config/config_test.go`

**实现内容**:
- 更新测试期望路径从 `cerberus.db` 到运行时路径
- 或者使用 `:memory:` 数据库进行测试
- 添加 Paths 字段验证

**验收标准**:
- 所有测试通过
- 测试验证路径符合预期

---

### Phase 3: Makefile 和构建

#### Task 3.1: 更新 Makefile
**目标**: 将构建产物移到 build/ 目录

**文件**: `Makefile`

**实现内容**:
- 更新 `build` 目标：输出到 `build/cerberus` 而不是 `bin/cerberus`
- 更新 `run` 目标：使用 `./build/cerberus`
- 更新 `clean` 目标：清理 `build/` 和 `runtime/`
- 更新 `coverage` 目标：输出到 `runtime/cover.out`
- 保持 bin/ 作为兼容性（可选：软链接）

**测试**:
- 手动测试 `make build` 生成 `build/cerberus`
- 手动测试 `make run` 正常运行
- 手动测试 `make clean` 清理正确

**验收标准**:
- `make build` 输出到 `build/cerberus`
- `make run` 正常运行
- `make clean` 清理所有运行时文件

---

### Phase 4: Gitignore 和清理

#### Task 4.1: 简化 .gitignore
**目标**: 简化 gitignore 规则

**文件**: `.gitignore`

**实现内容**:
```gitignore
# 构建产物
build/
bin/

# 运行时文件
runtime/

# 配置（仅敏感文件）
.cerberus/credentials.yaml

# IDE / 工具
.claude/
.codegraph/
.vscode/
.idea/
.opencraft/

# 旧文档位置
docs/
learning/

# 测试覆盖率
*.test
```

**测试**:
- 验证 `build/` 被忽略
- 验证 `runtime/` 被忽略
- 验证 `.cerberus/credentials.yaml` 被忽略
- 验证 `.cerberus/project.yaml` 不被忽略（如果需要版本控制）

**验收标准**:
- gitignore 规则正确
- 仅运行时文件被忽略
- 配置文件（非敏感）可被版本控制

---

### Phase 5: 向后兼容和文档

#### Task 5.1: 更新 CLAUDE.md
**目标**: 更新项目约束文档

**文件**: `.claude/CLAUDE.md`

**实现内容**:
- 更新 Commands 部分：`make build` 输出到 `build/cerberus`
- 添加运行时目录说明
- 更新 Key Files 部分：添加 `internal/runtime/`

**验收标准**:
- 文档准确反映新的目录结构
- 命令示例正确

---

#### Task 5.2: 创建技术文档
**目标**: 记录运行时文件管理机制

**文件**: `cerberus-docs/technical/runtime/2026-06-16-runtime-file-management.md`

**实现内容**:
- 开发时 vs 部署后路径对比表
- 环境检测机制
- 各平台标准路径
- 清理命令说明

**验收标准**:
- 文档清晰说明机制
- 包含所有平台路径示例

---

## 提交规范

每个任务完成后提交：
```bash
git add <affected-files>
git commit -m "<type>: <description>

- Change 1
- Change 2

Refs: cerberus-docs/superpowers/plans/2026-06-16-runtime-management-refactor.md" \
  --author="binoctal <binoctal@gmail.com>"
```

类型：
- `feat`: 新功能
- `fix`: 修复
- `refactor`: 重构
- `test`: 测试
- `chore`: 构建/文档

## 顺序执行

任务按顺序执行，每个任务完成后进入下一个。

## 测试策略

1. **单元测试**: 每个函数有对应测试
2. **集成测试**: Config 集成测试
3. **手动测试**: Makefile 命令手动验证
4. **跨平台**: 路径生成在各平台测试（通过构建标签）
