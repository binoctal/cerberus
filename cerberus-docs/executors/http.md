# HTTP Executor

The HTTP executor handles REST API interactions. It supports two action types:
`api_request` for full HTTP calls and `navigate` for simple URL fetching.

## Actions

### `api_request`

| Field     | Type              | Required | Description                       |
|-----------|-------------------|----------|-----------------------------------|
| `method`  | string            | yes      | HTTP method (`GET`, `POST`, etc.) |
| `url`     | string            | yes      | Target URL                        |
| `headers` | map[string]string | no       | Request headers                   |
| `body`    | string            | no       | Request body (JSON string)        |

When a `body` is provided without an explicit `Content-Type` header, the executor
automatically sets `Content-Type: application/json`.

**Success condition:** HTTP status 200-399. Response body is limited to 1 MB.

### `navigate`

| Field | Type   | Required | Description   |
|-------|--------|----------|---------------|
| `url` | string | yes      | URL to fetch  |

Equivalent to `api_request` with `method: GET`. Useful for rule-engine matching
when the test intent is "visit this page."

## Configuration

- Client timeout: **30 seconds**
- Response body limit: **1 MB**

## Examples

### GET request with header

```json
{
  "action_type": "api_request",
  "method": "GET",
  "url": "https://api.example.com/users",
  "headers": {
    "Authorization": "Bearer token123"
  }
}
```

### POST with JSON body

```json
{
  "action_type": "api_request",
  "method": "POST",
  "url": "https://api.example.com/users",
  "headers": {
    "Authorization": "Bearer token123"
  },
  "body": "{\"name\": \"Alice\", \"email\": \"alice@example.com\"}"
}
```

### Navigate to page

```json
{
  "action_type": "navigate",
  "url": "https://example.com/dashboard"
}
```

### PUT update

```json
{
  "action_type": "api_request",
  "method": "PUT",
  "url": "https://api.example.com/users/42",
  "headers": { "Authorization": "Bearer token123" },
  "body": "{\"name\": \"Alice Updated\"}"
}
```

## Result Fields

Every execution returns an `ExecutorResult` with:

- **Success** -- `true` if status 200-399
- **Duration** -- elapsed wall time
- **Summary** -- one-line status description (e.g. `HTTP 200 OK`)
- **Evidence** -- response status code, headers, and body (truncated at 1 MB)
