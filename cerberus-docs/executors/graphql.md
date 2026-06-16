# GraphQL Executor

The GraphQL executor sends queries and mutations to GraphQL endpoints via
the `graphql_query` action.

## Actions

### `graphql_query`

| Field          | Type              | Required | Description                |
|----------------|-------------------|----------|----------------------------|
| `url`          | string            | yes      | GraphQL endpoint URL       |
| `query`        | string            | yes      | GraphQL query or mutation  |
| `variables`    | map[string]any    | no       | Query variables            |
| `headers`      | map[string]string | no       | HTTP headers (for auth)    |
| `operationName`| string            | no       | Named operation to execute |

The executor sends an HTTP POST with `Content-Type: application/json`. The
request body contains `query`, `variables`, and `operationName` fields.

**Success condition:** HTTP 200 and no `errors` array in the response body.
Client timeout is **30 seconds**. Response body is limited to **1 MB**.

## Examples

### Simple query

```json
{
  "action_type": "graphql_query",
  "url": "https://api.example.com/graphql",
  "query": "{ users { id name email } }"
}
```

### Query with variables

```json
{
  "action_type": "graphql_query",
  "url": "https://api.example.com/graphql",
  "query": "query GetUser($id: ID!) { user(id: $id) { id name email } }",
  "variables": { "id": "42" },
  "operationName": "GetUser"
}
```

### Authenticated mutation

```json
{
  "action_type": "graphql_query",
  "url": "https://api.example.com/graphql",
  "headers": {
    "Authorization": "Bearer token123"
  },
  "query": "mutation CreateUser($input: CreateUserInput!) { createUser(input: $input) { id } }",
  "variables": {
    "input": { "name": "Alice", "email": "alice@example.com" }
  }
}
```

### Introspection query

```json
{
  "action_type": "graphql_query",
  "url": "https://api.example.com/graphql",
  "query": "{ __schema { types { name } } }"
}
```

## Result

- **Success** -- HTTP 200 with no `errors` in response
- **Evidence** -- full JSON response body (truncated at 1 MB)
- **Summary** -- status and whether errors were present
