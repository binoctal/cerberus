# Database Executor

The database executor validates data state through SQL queries and assertions.
It supports `db_query` for raw queries and `db_assert` for declarative checks.

## Supported Drivers

| Driver     | DSN Example                      | Notes                |
|------------|----------------------------------|----------------------|
| `sqlite`   | `:memory:` or `/path/to/db.sqlite` | Built-in, no CGo   |
| `postgres` | `postgres://user:pass@host/db`   | Requires driver      |
| `mysql`    | `user:pass@tcp(host:3306)/db`    | Requires driver      |

SQLite is available out of the box via `modernc.org/sqlite`. Other drivers
require the corresponding Go SQL driver at build time.

## Actions

### `db_query`

| Field    | Type       | Required | Description                     |
|----------|------------|----------|---------------------------------|
| `driver` | string     | yes      | One of `sqlite`, `postgres`, `mysql` |
| `dsn`    | string     | yes      | Data source name                |
| `query`  | string     | yes      | SQL query                       |
| `args`   | []any      | no       | Query parameters                |

Returns row data as evidence.

### `db_assert`

| Field       | Type   | Required | Description                                      |
|-------------|--------|----------|--------------------------------------------------|
| `driver`    | string | yes      | Database driver                                  |
| `dsn`       | string | yes      | Data source name                                 |
| `query`     | string | yes      | SQL query returning rows to check                |
| `assertion` | string | yes      | Assertion expression                             |

### Assertion Syntax

Assertions use `field operator value` syntax:

```
count == 0
rows.length > 0
price >= 100
status != "inactive"
total < 1000
```

**Special field:** `rows.length` resolves to the number of returned rows.
All other field names resolve from the first row's columns.

**Supported operators:** `==`, `!=`, `>`, `<`, `>=`, `<=`

## Examples

### Query SQLite in-memory

```json
{
  "action_type": "db_query",
  "driver": "sqlite",
  "dsn": ":memory:",
  "query": "SELECT 1 AS val"
}
```

### Assert row count

```json
{
  "action_type": "db_assert",
  "driver": "sqlite",
  "dsn": "file:test.db",
  "query": "SELECT COUNT(*) AS count FROM users WHERE active = 1",
  "assertion": "count > 0"
}
```

### Assert field value

```json
{
  "action_type": "db_assert",
  "driver": "sqlite",
  "dsn": "file:test.db",
  "query": "SELECT role FROM users WHERE id = 1",
  "assertion": "role == \"admin\""
}
```

### Query with parameters

```json
{
  "action_type": "db_query",
  "driver": "sqlite",
  "dsn": "file:test.db",
  "query": "SELECT * FROM orders WHERE user_id = ? AND total > ?",
  "args": [42, 100]
}
```

## Result

- **Success** -- query executed without error; for `db_assert`, assertion passes
- **Evidence** -- returned rows (for `db_query`) or assertion result details
