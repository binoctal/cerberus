# Qwen SSE Event Ordering Bug

**Date:** 2026-07-11
**Status:** Confirmed (deterministic, 5/5 reproductions)
**Severity:** High — breaks Claude Code subagents routed to Qwen
**Affected endpoint:** `https://moma.cmecloud.cn/v1/messages`
**Affected model:** `Qwen/Qwen3.7-Max`
**Unaffected model:** `ZHIPU/GLM-5.2`

## Summary

The `moma.cmecloud.cn` proxy emits Anthropic SSE events in the wrong order for
`Qwen/Qwen3.7-Max`. It sends `message_stop` **before** `message_delta`, violating
the Anthropic streaming protocol. Claude Code parses the stream strictly: upon
seeing `message_stop` it treats the stream as ended, but `message_delta` (which
carries `stop_reason`) has not arrived yet, so `stop_reason` is still `null`.
Claude Code synthesizes an error state (observed as a `stop_sequence`
stop reason) and the request fails.

Non-streaming requests succeed because they return a complete JSON object whose
`stop_reason` does not depend on event ordering. This is why `curl` against the
same token/endpoint always returned HTTP 200 while Claude Code still failed —
`curl` was used in non-streaming mode.

## Root Cause

Anthropic streaming protocol mandates this terminal ordering:

```
message_delta  (carries stop_reason, usage)
message_stop   (marks stream end)
```

The Qwen proxy at `moma.cmecloud.cn` emits:

```
message_stop
message_delta
```

— inverted. Confirmed stable across 5 consecutive streaming requests; the
inversion is deterministic, not a race.

GLM (`ZHIPU/GLM-5.2`) emits the correct order (`message_delta` then
`message_stop`), so requests routed to it always succeed.

## Reproduction

```bash
TOKEN="<redacted>"
for i in 1 2 3 4 5; do
  ord=$(curl -s --max-time 30 -N -X POST "https://moma.cmecloud.cn/v1/messages" \
    -H "Authorization: Bearer $TOKEN" -H "anthropic-version: 2023-06-01" \
    -H "anthropic-beta: interleaved-thinking-2025-05-14" \
    -H "Content-Type: application/json" \
    -d '{"model":"Qwen/Qwen3.7-Max","max_tokens":4096,"stream":true,
          "thinking":{"type":"enabled","budget_tokens":2000},
          "messages":[{"role":"user","content":"Say hello."}]}' \
    | grep -oE '"type":"(message_delta|message_stop)"' | paste -sd'→')
  echo "run $i: $ord"
done
# Output (all 5 runs):
#   run 1: message_stop → message_delta
#   run 2: message_stop → message_delta
#   run 3: message_stop → message_delta
#   run 4: message_stop → message_delta
#   run 5: message_stop → message_delta
```

### Full event sequence (Qwen, streaming)

```
message_start
content_block_start      (thinking)
content_block_delta ×132 (thinking_delta)
content_block_stop
content_block_start      (text)
content_block_delta ×3   (text_delta)
content_block_stop
message_stop             ← arrives BEFORE message_delta (BUG)
message_delta            ← stop_reason lives here; too late
```

### Correct sequence (GLM, streaming)

```
message_start
content_block_start      (thinking)
content_block_delta ×51  (thinking_delta)
content_block_stop
content_block_start      (text)
content_block_delta ×2   (text_delta)
content_block_stop
message_delta            ← correct: stop_reason first
message_stop             ← then stream end
```

## Impact on Claude Code

Configuration under test (`.claude/settings.json`):

```json
"env": {
  "ANTHROPIC_BASE_URL": "https://moma.cmecloud.cn/v1/chat/completions",
  "ANTHROPIC_DEFAULT_HAIKU_MODEL": "Qwen/Qwen3.7-Max",
  "ANTHROPIC_DEFAULT_SONNET_MODEL": "ZHIPU/GLM-5.2",
  "ANTHROPIC_DEFAULT_OPUS_MODEL": "Qwen/Qwen3.7-Max"
}
```

- **Main session** uses the SONNET slot → GLM → correct order → succeeds.
- **Subagents** (Task tool) and any Opus/Haiku-routed call use Qwen → inverted
  order → fail with a synthesized `stop_sequence` error.

## Workarounds

1. **Route OPUS/HAIKU to a known-good model** (immediate, verified path):
   Set both `ANTHROPIC_DEFAULT_OPUS_MODEL` and
   `ANTHROPIC_DEFAULT_HAIKU_MODEL` to `ZHIPU/GLM-5.2`. This is the only fix
   that works without touching the proxy.
2. **Report the bug upstream** to the `moma.cmecloud.cn` operator: the proxy
   must emit `message_delta` before `message_stop` to comply with the
   Anthropic streaming protocol.
3. **Non-streaming passthrough** — if the proxy supports `stream:false`
   passthrough for the Messages API, that avoids the ordering dependency.
   Unknown whether this proxy honors it.

## Not the Cause

- Token / auth — same Bearer token works for GLM and for non-streaming Qwen.
- Request shape — `thinking`, `tools`, `cache_control`, large system prompt,
  multiple `anthropic-beta` values all returned HTTP 200 in curl; none
  reproduced the failure because curl was non-streaming.
- Claude Code model routing — the failure is not Opus-specific routing; it is
  purely the model the slot resolves to (Qwen has the bug, GLM does not).
