# Installation

## Binary (Recommended)

Download from [GitHub Releases](https://github.com/binoctal/cerberus/releases):

```bash
# Linux/macOS
curl -sL https://github.com/binoctal/cerberus/releases/latest/download/cerberus_$(uname -s)_$(uname -m).tar.gz | tar xz
sudo mv cerberus /usr/local/bin/
```

## Go Install

```bash
go install github.com/binoctal/cerberus@latest
```

## From Source

```bash
git clone https://github.com/binoctal/cerberus.git
cd cerberus
make build
# Binary at bin/cerberus
```

## Verify

```bash
cerberus version
# cerberus v0.1.0 (commit: abc1234, built: 2026-06-12)
```

## Requirements

- **Anthropic API Key** — set `ANTHROPIC_API_KEY` environment variable
- **Optional**: Playwright (for browser testing), PostgreSQL/MySQL drivers (for database testing)
