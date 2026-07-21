# Project Configuration

The project configuration file is `.cerberus/project.yaml`. It defines services, actors, databases, invariants, and testing settings.

## Full Schema

```yaml
project:
  name: "my-project"          # Project name

services:                      # Target services
  - name: web                  # Required: service name
    url: "http://localhost:3000"  # Required: base URL
    health: "/"                # Optional: health check path
    headers:                   # Optional: headers injected on every request (matched by host:port)
      Host: api.example.com
  - name: api
    url: "http://localhost:8080"

actors:                        # User personas
  - name: admin                # Required: actor name
    credentials:
      email: "${ADMIN_EMAIL}"  # Env var interpolation supported
      password: "${ADMIN_PASS}"
      headers:                 # Optional: per-actor headers (e.g. Authorization: Bearer ...)
        Authorization: "Bearer ${API_KEY}"
    entry: "/admin"            # Optional: entry page

databases:                     # Database connections
  - name: main
    url: "${DATABASE_URL}"

code:                          # Code analysis config
  root: "."                    # Project root
  providers: ["go"]            # Language providers

invariants:                    # Business rules to verify
  - id: INV-001
    description: "balance cannot be negative"
    severity: critical         # low, medium, high, critical
    check: "SELECT COUNT(*) AS cnt FROM users WHERE balance < 0"
    assertion: "cnt == 0"

settings:
  max_duration: "30m"          # Total session timeout
  confidence_threshold: 0.7    # Min confidence to pass (0.0-1.0)
  auto_fix: "low_only"        # off, low_only, aggressive
  ai_budget:
    session_total_tokens: 200000
    per_call_limit: 10000
    model: "claude-sonnet-4-6"
  cost_alerts:
    warn_at_pct: 80            # Warn at 80% of budget
    stop_at_pct: 100           # Stop at 100% of budget
```

## Automatic Model Tier Assignment

When cerberus runs under Claude Code, it reads three model tiers from
`.claude/settings.json` and routes each head to the tier matching its task
complexity — no per-head config required:

| Env var | Tier | Used by |
|---|---|---|
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | fast | Agent (ReAct execute) |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | mid | Scout (plan), Examiner (judge) |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | strong | Critic (review) |

Per-head `settings.models.<head>` overrides still win when set; standalone runs
with no detected CLI keep the previous single-model behavior (`settings.ai_budget.model`).
To enable tiering, set the three `ANTHROPIC_DEFAULT_*_MODEL` values in
`.claude/settings.json` — e.g. point HAIKU at a cheaper model for the
high-frequency Agent.

## Environment Variables

Use `${VAR_NAME}` syntax for env var interpolation in any string field.

## Header Injection

For API gateways that route by `Host` and authenticate by `Authorization:
Bearer`, declare headers at two levels:

- `services[].headers` — shared across actors, matched by the service URL's
  `host:port`. Use for `Host` (domain routing), `X-Internal-Auth`, tracing ids.
- `actors[].credentials.headers` — per-actor (from the first actor). Use for
  `Authorization: Bearer ...`.

Both are injected at the **execution layer** on every HTTP request, so they
apply to both the rule-engine fast-path and the ReAct loop. Final priority is
**service < actor < action**: an action's own header overrides an injected one,
and setting an action header to an empty string *removes* an injected header
(needed for negative tests like "no Authorization → 401").

`Host` is routed to the request's `Host` field (not the header map), which
net/http requires for domain-routed gateways.

```yaml
services:
  - name: gateway
    url: "http://localhost:8081"
    headers:
      Host: api.opendune.com
actors:
  - name: valid_user
    credentials:
      headers:
        Authorization: "Bearer sk-relay-..."
  - name: anonymous
    credentials: {}
```

## WebSocket protocol

A service may declare its WebSocket protocol facts so the executor injects auth,
matches by the declared routing field, and frames messages deterministically
(see [WebSocket executor](../executors/websocket.md)). Declare it **inline**:

```yaml
services:
  - name: rt
    url: http://localhost:8787
    protocol:
      framing: json
      type_path: type
      auth: { strategy: query, param: token, credential_ref: web-actor }
```

…or **by reference** to a standalone, shareable file under
`.cerberus/protocols/<name>.yaml`:

```yaml
services:
  - name: rt
    url: http://localhost:8787
    protocol_ref: open-agents
```

```yaml
# .cerberus/protocols/open-agents.yaml — version-controlled, shareable across projects
framing: json
type_path: type
auth:
  strategy: query
  param: token
  credential_ref: web-actor
roles:
  web: { credential_ref: web-actor, params: { type: web } }
```

`protocol` and `protocol_ref` are mutually exclusive. The reference is a plain
name (no path separators or `..`); the file is loaded at config-load time. Full
field semantics (framing, type_path, auth, roles, handshake, assertions) are in
the [WebSocket executor doc](../executors/websocket.md).

## Validation

Cerberus validates the config on load and reports all issues:
- Duplicate service/actor/database/invariant names
- Invalid URLs, durations, severity levels
- Confidence threshold range (0.0-1.0)
- Budget value constraints
