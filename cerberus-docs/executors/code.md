# Code Executor

The code executor performs language-aware static analysis. It supports three
action types: `code_analyze`, `code_lint`, and `code_symbols`.

## Actions

### `code_analyze`

Run static analysis checks on source code.

| Field        | Type       | Required | Description                          |
|--------------|------------|----------|--------------------------------------|
| `targetPath` | string     | yes      | File or directory to analyze         |
| `language`   | string     | no       | `go`, `python`, `javascript`, `typescript` |
| `checks`     | []string   | no       | Checks to run (default: all)         |

**Built-in Go checks:**

| Check             | Description                              |
|-------------------|------------------------------------------|
| `complexity`      | Cyclomatic complexity (threshold: 15)    |
| `unhandled_error` | Functions returning error without check   |
| `dead_code`       | Unreachable or unused code               |

**Other languages** delegate to external tools:

- Python → `ruff check --output-format json`
- JavaScript/TypeScript → `npx eslint --format json`

### `code_lint`

Lint source code with configurable rules.

| Field        | Type       | Required | Description                     |
|--------------|------------|----------|---------------------------------|
| `targetPath` | string     | yes      | File or directory to lint       |
| `language`   | string     | no       | Language hint                   |
| `rules`      | []string   | no       | Specific lint rules to enable   |

### `code_symbols`

Extract symbol definitions (functions, types, interfaces) from source files.

| Field        | Type   | Required | Description                    |
|--------------|--------|----------|--------------------------------|
| `targetPath` | string | yes      | File or directory              |
| `language`   | string | no       | Language hint                  |

## Configuration

- Runs within the sandbox environment
- Go analysis uses built-in AST parsing (no external tools)
- Default checks when none specified: `["complexity", "unhandled_error", "dead_code"]`

## Examples

### Analyze Go code

```json
{
  "action_type": "code_analyze",
  "targetPath": "./internal/head/",
  "language": "go",
  "checks": ["complexity", "unhandled_error"]
}
```

### Lint Python code

```json
{
  "action_type": "code_lint",
  "targetPath": "./src/",
  "language": "python"
}
```

### Extract symbols

```json
{
  "action_type": "code_symbols",
  "targetPath": "./cmd/cerberus/main.go",
  "language": "go"
}
```

## Result Fields

- **Success** — `true` if no errors found
- **Summary** — count of issues by severity
- **Evidence** — full list of findings with file, line, and message
