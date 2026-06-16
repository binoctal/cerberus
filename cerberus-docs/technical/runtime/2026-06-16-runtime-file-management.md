# Runtime File Management

**Date**: 2026-06-16  
**Status**: Implemented (Scheme 3)

## Overview

Cerberus uses a simple, project-local runtime file management scheme. All runtime files (database, logs, cache) are stored in `.cerberus/runtime/` under the project directory. This approach:

- ✅ Simplifies cross-platform support (no platform-specific paths)
- ✅ Makes projects self-contained (everything in project directory)
- ✅ Supports multiple projects on same machine (isolated runtimes)
- ✅ Works everywhere without installation (no system-wide paths)

## Directory Structure

```
my-project/
├── .cerberus/
│   ├── project.yaml           # Project definition (version control)
│   ├── credentials.yaml       # API credentials (gitignore)
│   └── runtime/
│       ├── data/
│       │   └── cerberus.db    # SQLite database
│       ├── logs/               # Log files
│       └── cache/              # Temporary cache
└── src/                        # Your code
```

## Implementation

### Core Package: `internal/runtime/`

#### `paths.go`

```go
// Paths holds all runtime file paths for Cerberus
type Paths struct {
    ConfigDir   string  // .cerberus/
    DataDir     string  // .cerberus/runtime/data
    LogsDir     string  // .cerberus/runtime/logs
    CacheDir    string  // .cerberus/runtime/cache
    DBPath      string  // .cerberus/runtime/data/cerberus.db
    ProjectRoot string  // Project directory
}

// New creates runtime paths for a project directory
func New(projectRoot string) *Paths

// Ensure creates all runtime directories
func (p *Paths) Ensure() error
```

#### `detect.go`

```go
// IsDevelopment detects if running in cerberus development environment
func IsDevelopment() bool

// GetPaths returns runtime paths based on current project directory
func GetPaths() *Paths
```

### Usage Example

```go
import "github.com/binoctal/cerberus/internal/runtime"

// Get paths for current project
paths := runtime.GetPaths()

// Ensure directories exist
paths.Ensure()

// Use paths
dbPath := paths.DBPath        // .cerberus/runtime/data/cerberus.db
logsDir := paths.LogsDir      // .cerberus/runtime/logs
```

## Design Decisions

### Why Project-Local Storage? (Scheme 3)

**Rejected alternatives**:

- **Scheme 1** (System-wide paths):
  - ❌ Complex platform detection (Linux, macOS, Windows, Docker)
  - ❌ Requires installation/setup
  - ❌ Harder to support multiple projects
  
- **Scheme 2** (XDG Base Directory Specification):
  - ❌ Platform-specific (Linux only)
  - ❌ Files scattered across system
  - ❌ Harder to debug/find files

**Accepted Scheme 3** (Project directory):
  - ✅ Simple implementation (62 lines vs 157 lines)
  - ✅ Works everywhere immediately
  - ✅ Self-contained projects
  - ✅ Easy to understand/debug

### Trade-offs

**Pros**:
- Zero configuration (works out of the box)
- Projects are self-contained (copy folder = move project)
- Simple codebase (less platform-specific logic)
- Easy to clean up (delete `.cerberus/`)

**Cons**:
- Runtime files in project directory (might clutter some workflows)
- No shared cache between projects (each has own cache)

## Git Configuration

### `.gitignore`

```gitignore
# Build artifacts
build/
bin/

# Project runtime files
.cerberus/runtime/

# Cerberus credentials (sensitive)
.cerberus/credentials.yaml
```

### What to Version Control

- ✅ `.cerberus/project.yaml` - Project definition
- ❌ `.cerberus/credentials.yaml` - API credentials
- ❌ `.cerberus/runtime/` - Runtime data/logs/cache

## Testing

The `internal/runtime` package has comprehensive tests:

```bash
go test -v ./internal/runtime/...
```

All tests verify project-local paths (`.cerberus/runtime/`).

## References

- Implementation: `internal/runtime/paths.go`, `internal/runtime/detect.go`
- Tests: `internal/runtime/paths_test.go`
- Original plan: `cerberus-docs/superpowers/plans/2026-06-16-runtime-management-refactor.md`

## Summary

Cerberus uses project-local runtime storage (`.cerberus/runtime/`) for simplicity and cross-platform compatibility. This approach eliminates platform-specific complexity and makes projects self-contained.
