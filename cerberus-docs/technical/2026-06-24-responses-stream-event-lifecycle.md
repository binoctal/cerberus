# /v1/responses 流式事件生命周期说明

## 概述

OpenAI Responses API 流式协议中，文本输出的事件生命周期及 `output_text.delta` 的作用。

## 文本流式事件序列

当模型生成文本内容时，事件按如下顺序推送：

```
response.content_part.added          ← 文本块开始
response.output_text.delta (text片段)  ← 逐块推送（0~N 次）
response.output_text.done  (完整text)  ← 文本完成
response.content_part.done           ← 文本块关闭
```

### `response.output_text.delta`

流式传输中**逐块推送文本片段**的事件。模型每生成一个或几个 token 就立即推送一个 delta，客户端可据此实时渲染打字效果。

## Function Call 场景

当模型决定调用工具时，function call 有独立的事件序列：

```
response.output_item.added (type: function_call)
response.function_call_arguments.delta   ← 参数片段（0~N 次）
response.function_call_arguments.done    ← 参数完成
response.output_item.done
```

## 空文本块是合法行为

模型可以选择**不输出文本直接调用工具**。此时服务端仍会发出一个空文本 content_part，生命周期完整但无 delta：

```
content_part.added   (text: "")
output_text.done     (text: "")    ← 无 delta，因为文本确实为空
content_part.done    (text: "")
```

这**不是 bug**，是协议允许的合法行为。

## UAT 实测验证（2026-06-24）

### 测试环境

- Base URL: `http://llm-uat.sirayatech.com/v1`
- 测试脚本: `runtime/debug/test_responses_stream_bug.py`

### 测试结果

| 场景 | 模型 | tools | output_text.delta | 结论 |
|------|------|-------|-------------------|------|
| 带 tools | claude-sonnet-4.6 | ✅ | 无（text=""） | 正常 — 模型直接调工具不说话 |
| 带 tools | claude-opus-4.7 | ✅ | 有（3个delta） | 正常 — 模型先说话再调工具 |
| 无 tools | claude-sonnet-4.6 | ❌ | 有（4个delta） | 正常 |
| 无 tools | claude-opus-4.7 | ❌ | 有（7个delta） | 正常 |

### 完整事件流示例：claude-sonnet-4.6 + tools（空文本 + function call）

```json
// Event 0: 响应创建
{"type": "response.created", "response": {"id": "resp_...", "status": "in_progress"}}

// Event 1: 响应进行中
{"type": "response.in_progress", ...}

// Event 2: message output_item 开始
{"type": "response.output_item.added", "item": {"type": "message", "role": "assistant"}, "output_index": 0}

// Event 3: 空文本 content_part 开始
{"type": "response.content_part.added", "part": {"type": "output_text", "text": ""}, "output_index": 0}

// Event 4: function_call output_item 开始
{"type": "response.output_item.added", "item": {"type": "function_call", "name": "get_weather"}, "output_index": 1}

// Event 5-7: function call 参数流式推送
{"type": "response.function_call_arguments.delta", "delta": "{\"city\": \"Paris\"}", "output_index": 1}

// Event 8: 空文本完成（无 delta 因为 text 为空）
{"type": "response.output_text.done", "text": "", "output_index": 0}

// Event 9: 空文本块关闭
{"type": "response.content_part.done", "part": {"type": "output_text", "text": ""}, "output_index": 0}

// Event 10: message item 完成
{"type": "response.output_item.done", "item": {"type": "message", "content": [{"type": "output_text", "text": ""}]}, "output_index": 0}

// Event 11: function call 参数完成
{"type": "response.function_call_arguments.done", "arguments": "{\"city\": \"Paris\"}", "output_index": 1}

// Event 12: function_call item 完成
{"type": "response.output_item.done", "item": {"type": "function_call", "name": "get_weather", "arguments": "{\"city\": \"Paris\"}"}, "output_index": 1}

// Event 13: 响应完成
{"type": "response.completed", "response": {"status": "completed", "output": [...]}}
```

### 关键结论

1. `output_text.delta` 缺失 ≠ bug，需检查 `output_text.done` 中 `text` 是否为空
2. 空文本块（`text: ""`）+ 完整生命周期事件是合法的协议行为
3. 真正的 bug 应检测：有非空 delta 推送但 `output_text.done` 中 text 不一致或缺失
