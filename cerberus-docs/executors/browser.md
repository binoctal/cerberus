# Browser Executor

The browser executor automates web UI interactions using Playwright. It is an
optional plugin -- registered only when the Playwright browser binary is
available.

## Prerequisites

Install Playwright and its Chromium browser:

```bash
# Playwright must be available in your Go environment
# The executor auto-detects availability at startup
```

The executor uses headless Chromium via `playwright-community/playwright-go`.

## Actions

### `browser_goto`

| Field        | Type   | Required | Description                                          |
|--------------|--------|----------|------------------------------------------------------|
| `url`        | string | yes      | URL to navigate to                                   |
| `wait_until` | string | no       | Wait condition: `load`, `domcontentloaded`, `networkidle` |

Default `wait_until` is `load`.

### `browser_click`

| Field      | Type   | Required | Description                        |
|------------|--------|----------|------------------------------------|
| `selector` | string | no       | CSS selector to click              |
| `text`     | string | no       | Click element containing this text |

One of `selector` or `text` must be provided.

### `browser_fill`

| Field      | Type   | Required | Description              |
|------------|--------|----------|--------------------------|
| `selector` | string | yes      | CSS selector for input   |
| `value`    | string | yes      | Value to fill            |

### `browser_eval`

| Field       | Type   | Required | Description              |
|-------------|--------|----------|--------------------------|
| `expression`| string | yes      | JavaScript to evaluate   |

## Examples

### Login flow

```json
[
  {
    "action_type": "browser_goto",
    "url": "https://example.com/login",
    "wait_until": "networkidle"
  },
  {
    "action_type": "browser_fill",
    "selector": "#email",
    "value": "user@example.com"
  },
  {
    "action_type": "browser_fill",
    "selector": "#password",
    "value": "s3cret"
  },
  {
    "action_type": "browser_click",
    "selector": "button[type=submit]"
  }
]
```

### Navigate and extract

```json
{
  "action_type": "browser_goto",
  "url": "https://example.com/dashboard"
}
```

```json
{
  "action_type": "browser_eval",
  "expression": "document.querySelector('.status').textContent"
}
```

### Click by text

```json
{
  "action_type": "browser_click",
  "text": "Sign Out"
}
```

## Screenshots

The executor exposes a `TakeScreenshot()` method that returns a base64-encoded
PNG. This is used internally by the Agent head for evidence collection.

## Result

- **Success** -- action completed without error
- **Evidence** -- page body text (truncated at 5000 runes), screenshot (base64 PNG)
- **Duration** -- time including page wait
