# File Executor

The file executor reads, writes, and checks files within the project directory.
All paths are resolved relative to the project root.

## Path Safety

The executor enforces path traversal protection. Any path containing `..` that
would escape the project root is rejected.

## Actions

### `file_read`

| Field  | Type   | Required | Description        |
|--------|--------|----------|--------------------|
| `path` | string | yes      | Relative file path |

Reads and returns file contents.

### `file_write`

| Field     | Type   | Required | Description         |
|-----------|--------|----------|---------------------|
| `path`    | string | yes      | Relative file path  |
| `content` | string | yes      | Content to write    |

Creates parent directories if needed. Uses file mode `0644`.

### `file_exists`

| Field  | Type   | Required | Description        |
|--------|--------|----------|--------------------|
| `path` | string | yes      | Relative file path |

Returns whether the file or directory exists.

### `file_glob`

| Field     | Type   | Required | Description              |
|-----------|--------|----------|--------------------------|
| `pattern` | string | yes      | Glob pattern to match   |

Returns list of matching file paths.

## Examples

### Read a config file

```json
{
  "action_type": "file_read",
  "path": "config/app.yaml"
}
```

### Check file exists

```json
{
  "action_type": "file_exists",
  "path": ".cerberus/project.yaml"
}
```

### Write a report

```json
{
  "action_type": "file_write",
  "path": "reports/test-output.txt",
  "content": "All tests passed at 2026-06-13T12:00:00Z"
}
```

### Find Go test files

```json
{
  "action_type": "file_glob",
  "pattern": "**/*_test.go"
}
```

### Verify migration files

```json
{
  "action_type": "file_exists",
  "path": "migrations/001_init.sql"
}
```

### Read and validate dotenv

```json
{
  "action_type": "file_read",
  "path": ".env"
}
```

## Result

| Action         | Success             | Evidence                  |
|----------------|---------------------|---------------------------|
| `file_read`    | File exists         | File contents             |
| `file_write`   | Write succeeds      | Bytes written, path       |
| `file_exists`  | Always (reports bool)| Existence boolean         |
| `file_glob`    | Always              | List of matched paths     |

## Notes

- All paths are joined with the project root passed during executor construction
- `file_write` triggers the escalation gate's `destructive_risk` checkpoint
- Symlinks that resolve outside the project root are rejected
