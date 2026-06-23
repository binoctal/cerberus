# Local Deploy Discovery + Multi-Service Routing — Design

- Date: 2026-06-23
- Status: Draft
- Author: binoctal

## Goal

让 cerberus 能测试像 modelsite 这样「本地 docker-compose 部署的微服务项目」:
1. 自动发现本地服务的端口与 health(替代手写 `project.yaml` 的 services)。
2. 执行层能把请求正确路由到不同 service,而非只打 `Services[0]`。

## Background

两个现状瓶颈(在对 modelsite 的实测中暴露):

- **执行层单 baseURL**:`Scout.resolveBaseURL()` 硬编码返回 `Services[0].URL`(`internal/head/scout/direct_planning.go:157`);`NewRuleEngine(baseURL)` 只接受单个 baseURL(`internal/head/agent/rules.go:22`)。`project.yaml` 列再多 service,规划层与 rule-engine 执行层都只把 `Services[0]` 当 baseURL,其余 service 的 URL 不会被发起请求。
- **配置全手写**:cerberus 不读 `docker-compose.yml`、不扫端口,service 的 URL/health 全靠人填 `project.yaml`。

## Scope

方案 C(打包)= 块① + 块②,有依赖顺序(② 建在 ① 之上):

- **块① 多 service 路由(核心,先做)**:解开单 baseURL 死结。
- **块② `discover` 命令(便利层,后做)**:解析 docker-compose.yml,生成/补全 `project.yaml` 的 services 段。

## Out of Scope

- **domain(多租户标签)、auth key 的自动发现**:存在于目标项目的数据库(如 modelsite 的 `agent_domains` / `api_keys` 表)。让 cerberus 去读会耦合目标项目 schema,违背通用工具定位。discover 只留空 + 提示,由人手填。
- **endpoint `path_prefix` 的自动发现**:`docker-compose.yml` 无 API 路径信息,discover 推不出。归属靠人配前缀(A)+ LLM 验证。
- **运行态探活(`docker ps`)**:第一版纯静态解析 compose,不引 docker CLI 依赖。

## 块① 多 service 路由

### 数据模型(新增 service 归属)

| 类型 | 新增字段 | 语义 |
|---|---|---|
| `project.EndpointDef` | `Service string` | endpoint 归属哪个 service |
| `agent.TestCase` | `Service string` | 用例归属哪个 service |
| `project.Actor` | `Service string`(可选) | 空 = 全局 actor;非空 = 该 service 专属 key/auth |
| `project.Service` | `PathPrefix []string`(可选) | A 的确定性归属源(如 `["/v1","/v1/models"]`) |

**向后兼容**:`Service` / `PathPrefix` 为空时,全部回退 `Services[0]`,现有单 service 配置行为不变。

### RuleEngine 改造(单 baseURL → 按 service 选)

- 持 `services map[string]project.Service` 替代单 `baseURL` 字段。
- `Match(tc)`:
  - `baseURL = services[tc.Service].URL`(`tc.Service` 空 → `Services[0]`)。
  - headers = `service.Headers` + 选中 actor 的 auth headers。
- **actor 选取**:先找 `actor.Service == tc.Service` 的专属 actor;无则回退全局 / `actors[0]`。
- `withBaseURL`(已有)改为按 `tc.Service` 取 base;为此归一化入口需从 `executeAndRecordAction` / `tryRecovery` 透传 `tc`(已可拿到)。

### Scout 改造 + LLM 验证层

- `resolveBaseURL()`(单)→ 规划时按 `PathPrefix` 把每个 endpoint 归到 service,填 `tc.Service`。
- **path_prefix 未配时的降级**:某 service 无 `PathPrefix` → 前缀主干缺失,该 endpoint 归属由 LLM 验证层直接推断(标 low-confidence),拿不准则回退 `Services[0]` + 日志。单 service 项目通常不配前缀,所有 case 回退 `Services[0]`(等价现有行为)。
- **LLM 验证层**:`tc.Service` 填好后(前缀给出或推断给出),LLM 拿 endpoint path + 各 service 的 `name` + `PathPrefix` 做一次校验:
  - 对 → 保留;错 → 从已知 services 纠偏;拿不准 → 标 low-confidence,回退前缀归属(或 `Services[0]`)+ 打日志。
  - 批量送检(一次校验多个 endpoint)省 token。

### per-service auth

`Actor.Service` 字段让每个 service 各自的 bearer / Host header 通过 `tc.Service` 正确注入(如 gateway 用 gateway 的 key,admin 用 admin 的 key)。

## 块② discover 命令

### 命令

```
cerberus discover [--compose <path>] [--dry-run] [--include <svc>...] [--exclude <svc>...]
```

默认读 `./docker-compose.yml`,`--compose` 可指向 `.prod.yml` 等。

### 流程

1. 用 `gopkg.in/yaml.v3`(go.mod 已有)读 compose 文件。
2. 遍历 `services`,过滤:
   - image 名命中 infra 黑名单(`postgres/redis/kafka/mysql/mongo/nginx/...`)→ 丢。
   - 无 `ports` 映射(只 `expose`,宿主连不到)→ 丢 + 提示。
   - `--include` / `--exclude` 强制覆盖。
3. 映射:
   - `ports: "8081:8080"` 或短语法 `"8081"` → `url = http://localhost:<宿主端口>`。
   - `healthcheck.test`(解析 `curl/wget .../path`)→ 提取 `health` 路径;提不出留空。
4. 合并写回 `project.yaml`:按 `name` 去重——**已存在的 service 保留手写 override**(domain/key/path_prefix 不覆盖),只补缺失字段;新 service 追加 `{name, url, health}`,其余留空。
5. 打印结果 + 缺口清单(每个 service 缺 domain/key/path_prefix 的提示)。
6. `--dry-run`:只打印不写。

discover **不参与 endpoint 归属**(compose 无 API 路径信息),只产出 `name/url/health`。

## Data Flow

```
cerberus discover  →  project.yaml(多 service:name/url/health 已填;domain/key/path_prefix 留空)
cerberus run       →  Scout 按 path_prefix + LLM 验证填 tc.Service
                   →  rule engine 按 tc.Service 选 URL + headers + actor
                   →  打到正确 service
```

## Error Handling

- **路由层降级链(永不崩溃)**:`tc.Service` 在 `services` map 找不到 → 回退 `Services[0]` + 警告;专属 actor 缺 → 全局 actor;LLM 验证拿不准 → 回退前缀归属 + 日志。
- **向后兼容**:旧单 service 配置(`Service` 字段空)→ 回退 `Services[0]`,行为不变。
- **discover**:compose 缺失 / 解析失败 → 报错退出;全部 service 被过滤 → 报错 + `--include` 提示;`health` 提不出 → 留空 + 提示(降级);merge 写回失败 → 建议先 `--dry-run`。
- **infra 误判**:黑名单漏判时,`--include <svc>` 强制保留。

## Testing (TDD)

复现测试先红后绿,复用现有 `testLoop`(mock LLM)+ `httptest.Server`。

- **块①**:
  - `tc.Service=router` → rule engine 打 `router.URL`,非 `Services[0]`。
  - per-service actor:`tc.Service=admin` → 注入 admin 专属 actor 的 auth,不注 gateway 的。
  - `tc.Service=""` → 回退 `Services[0]`(向后兼容)。
  - LLM 验证:前缀归属错时 mock LLM 纠正 `tc.Service`。
  - `withBaseURL` 按 `tc.Service` 取 base。
- **块②**:
  - 解析 compose → 正确 services(infra 被过滤)。
  - `ports "8081:8080"` → `url http://localhost:8081`。
  - `healthcheck.test` → 提取 `/health`。
  - merge:已存在 service 不覆盖手写 domain。
  - 缺口清单打印;`--dry-run` 不写文件。

## Decisions

- **工作模式**:生成 / 补全 `project.yaml`(半自动,可审改)。
- **缺口(domain/key)**:留空 + 提示,不耦合目标 DB。
- **触发**:新命令 `cerberus discover`(一次性手动,非运行时自动)。
- **endpoint → service 归属**:A(`path_prefix` 确定性主干)+ LLM 验证层;discover 不参与。
- **per-service auth**:第一版全做(`Actor.Service` 字段)。
