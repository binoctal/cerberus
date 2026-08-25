# Web UI 实况演示 — 编排效果肉眼可见

准备（两个终端）：

```bash
# 终端 1: API (wrangler, :8989)
cd open-agents/apps/api && eval "$(fnm env)" && fnm use 22 && npm run dev

# 终端 2: Web (Vite dev server)
cd open-agents/apps/web && eval "$(fnm env)" && fnm use 22 && npm run dev
# 打开它打印的地址（通常 http://localhost:5173），用 dev@openagents.local / dev123456 登录
```

起 3+1 个真实桥（另开一个终端，可重复执行换不同 -n 名）：

```bash
cd open-agents/bridge
HOME=/tmp/demo-b1 ./build/open-agents-bridge pair --dev --server http://localhost:8989 -n demo-b1
HOME=/tmp/demo-b1 ./build/open-agents-bridge start -d demo-b1   # 前台，保持运行
# demo-b2 / demo-b3 同样（各自独立 HOME）
```

在 UI 的 Missions 页新建 mission，DeviceIds 选三个 demo 桥，输入类似：
"Split into three independent subtasks; each replies done. Do not create files."
然后观察（真正的编排效果）：
1. planner（真 glm-4.5-air）实时分解出任务图
2. 每个 subtask 分派到不同设备（任务卡上的设备标识）
3. 任务执行流式输出（真 ACP/真 LLM 若把某桥的 agent 设为 claude）
4. 完成 → 合并 → mission 状态流转

多设备 fan-out（run 17 起修复的 round-robin）会肉眼可见：三个任务落在三个桥上。

## 演示结果（2026-08-26 补记）

Mission `job_1787672167461` 完整走完生命周期（API 创建 → 分解 3 任务 →
扇出到 b2/b3 → t1 卡 running 被 stuck-recovery 救回 → mission completed）。
UI 侧验证：

- **Mission 列表**：50 个历史 mission 全部渲染（Completed 28 / Partial 12 /
  Running 8 / Failed 2），Orchestration 统计 178 tasks / 51% success
- **任务流程图**：3 节点全 ✓；详情抽屉含真实会话输出（FAKE_codex_ECHO
  转录）与 Timeline（Created→Started→Completed）
- **坑**：mission 列表在左侧栏 `hidden md:flex`——窄窗口（<768px）走移动端
  布局看不到列表，视口调到 ≥768px 即可

### "Connecting..." / "0/0 online" 根因（非连接故障）

页面注入 WebSocket 探针实测：app 的 `ws://localhost:8989/ws/<user>` 正常
`open`；`/api/devices` 显示三桥 online:true。两个显示缺陷：

1. **#18**：侧边栏读 `appStore.isConnected`，但只有 `websocketStore` 被更新
   ——appStore 的字段无人写，永远 false
2. **#19**：WebSocketProvider 首次 `ensureValidToken()` 失败即永久放弃
   （无重试）；且 `refreshWithMutex` 无 try/finally，refresh 抛错后互斥锁
   卡死，同页所有后续 ensureValidToken 永久挂起（网关抖动当晚复现）

两条均已立案 open-agents known-issues #18/#19（对 dogfood 无影响）。
