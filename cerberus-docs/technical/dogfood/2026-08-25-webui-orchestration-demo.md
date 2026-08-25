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
