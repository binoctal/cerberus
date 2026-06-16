# Runtime File Management

**Date**: 2026-06-16
**Status**: Implemented
**Related**: cerberus-docs/superpowers/plans/2026-06-16-runtime-management-refactor.md

## Overview

Cerberus implements a cross-platform runtime file management system that automatically detects whether it's running in a development environment or production deployment, and places runtime files accordingly.

## Architecture

### Environment Detection

**`internal/runtime/detect.go`** provides automatic environment detection:

```go
// IsDevelopment checks if running in cerberus project directory
// 1. Checks for go.mod in current directory
// 2. Verifies module name is "github.com/binoctal/cerberus"
func IsDevelopment() bool

// GetPaths returns appropriate paths based on environment
// - Development: project-local paths
// - Production: system-standard paths
func GetPaths() *Paths
```

### Path Management

**`internal/runtime/paths.go`** provides cross-platform path resolution:

```go
type Paths struct {
    ConfigDir string  // Configuration files
    DataDir   string  // Persistent data (database)
    LogsDir   string  // Log files
    CacheDir  string  // Temporary cache
    DBPath    string  // SQLite database file
}
```

## Directory Structures

### Development Environment

When running from source (detected by `go.mod` presence):

```
cerberus/                    # Project root
├── build/                   # Build artifacts (gitignore)
│   └── cerberus            # Compiled binary
├── runtime/                 # Runtime files (gitignore)
│   ├── data/
│   │   └── cerberus.db    # SQLite database
│   ├── logs/
│   │   ├── cerberus.log   # Application logs
│   │   └── autotest.log   # AutoTest logs
│   └── cache/
│       └── sessions/      # Temporary session cache
└── .cerberus/              # Configuration (gitignore partial)
    ├── project.yaml        # Project config (can version control)
    └── credentials.yaml    # Credentials (never version control)
```

### Production Environment

When installed (no `go.mod` detected):

#### Linux (XDG Base Directory Specification)

```bash
# Configuration
~/.config/cerberus/
├── project.yaml
└── credentials.yaml

# Data
~/.local/share/cerberus/
└── data/
    └── cerberus.db

# Logs
~/.local/state/cerberus/
├── cerberus.log
└── autotest.log

# Cache
~/.cache/cerberus/
└── sessions/
```

**Environment Variable Overrides:**
- `XDG_CONFIG_HOME` — Default: `~/.config`
- `XDG_DATA_HOME` — Default: `~/.local/share`
- `XDG_CACHE_HOME` — Default: `~/.cache`
- Logs always use `~/.local/state` (not XDG overridable)

#### macOS

Same as Linux (follows XDG standard):
```bash
~/.config/cerberus/        # Config
~/.local/share/cerberus/   # Data
~/.local/state/cerberus/   # Logs
~/.cache/cerberus/         # Cache
```

#### Windows

```cmd
%APPDATA%\Cerberus\        # Configuration
├── project.yaml
└── credentials.yaml

%LOCALAPPDATA%\Cerberus\   # Data, Logs, Cache
├── data\
│   └── cerberus.db
├── logs\
│   ├── cerberus.log
│   └── autotest.log
└── cache\
    └── sessions\
```

#### Docker Container

```bash
/app/config/               # Configuration (volume mount)
├── project.yaml
└── credentials.yaml

/app/data/                 # Data (volume mount)
└── cerberus.db

/app/logs/                 # Logs (volume mount)
├── cerberus.log
└── autotest.log

/app/cache/                # Cache (volume mount)
└── sessions/
```

**Docker Detection**: Checks for `/.dockerenv` file presence.

## Usage

### Application Code

```go
import "github.com/binoctal/cerberus/internal/runtime"

// Get paths (auto-detects environment)
paths := runtime.GetPaths()

// Ensure directories exist
paths.Ensure()

// Use paths
dbPath := paths.DBPath              // Database location
logPath := filepath.Join(paths.LogsDir, "app.log")
configPath := filepath.Join(paths.ConfigDir, "project.yaml")
```

### Configuration Integration

**`internal/config/config.go`** automatically integrates runtime paths:

```go
cfg := config.Load()
// cfg.Paths is initialized
// cfg.DBPath defaults to cfg.Paths.DBPath
```

Override with environment variable:
```bash
export CERBERUS_DB_PATH=/custom/location/cerberus.db
```

## Benefits

1. **Automatic**: No configuration needed, auto-detects environment
2. **Standard**: Follows platform conventions (XDG, Windows, Docker)
3. **Clean**: `rm -rf runtime/` cleans all development artifacts
4. **Portable**: Works across Linux, macOS, Windows, Docker
5. **User-Friendly**: All files in user home directory for production

## Migration Notes

### For Users

No changes needed! Cerberus automatically:
1. Detects your environment
2. Uses appropriate directory structure
3. Creates directories as needed

Old files in project root (cerberus.db, etc.) can be deleted:
```bash
make clean  # Removes build/ and runtime/
```

### For Developers

Build output location changed:
```bash
# Old
make build  # → bin/cerberus

# New
make build  # → build/cerberus
```

Database location changed (for development):
```bash
# Old
./cerberus  # → ./cerberus.db

# New
make build && ./build/cerberus  # → runtime/data/cerberus.db
```

## Implementation Details

### Path Resolution Priority

1. **Development environment**: `runtime/` in project root
2. **Production**: System-standard paths (see above)
3. **Docker**: `/app/` paths
4. **Override**: `CERBERUS_DB_PATH` environment variable (always wins)

### Directory Creation

`paths.Ensure()` creates:
- `ConfigDir` — Configuration directory
- `DataDir/data/` — Database subdirectory
- `LogsDir` — Log directory
- `CacheDir` — Cache directory

Failure to create directories logs warning but continues (database open will fail with clear error if needed).

### Docker Detection

```go
func isInDocker() bool {
    if _, err := os.Stat("/.dockerenv"); err == nil {
        return true
    }
    return false
}
```

## Testing

### Unit Tests

**`internal/runtime/paths_test.go`**:
- Cross-platform path generation (Linux, macOS, Windows, Docker, Dev)
- Directory creation (`Ensure()`)
- Environment variable overrides

**`internal/runtime/detect_test.go`**:
- Development environment detection
- Path selection based on environment
- String matching helper

### Integration Tests

**`internal/config/config_test.go`**:
- Config loads with runtime paths
- DBPath defaults correctly
- Environment variable overrides work

### Manual Testing

```bash
# Development
make build && ./build/cerberus
# Check: runtime/data/cerberus.db exists

# Production simulation
cd /tmp
git clone <repo> cerberus
cd cerberus
go build && ./cerberus
# Check: ~/.local/share/cerberus/data/cerberus.db exists

# Docker
docker run -v /data:/app/data cerberus
# Check: /data/cerberus.db exists
```

## Future Enhancements

Potential improvements:
1. **Windows AppData**: Follow FHS (File System Hierarchy Standard) more closely
2. **Custom Runtime Root**: `CERBERUS_RUNTIME_ROOT` environment variable
3. **Multi-Instance**: Support multiple named instances via config
4. **Log Rotation**: Automatic log file rotation in LogsDir
5. **Cache Cleanup**: Automatic cache expiration and cleanup

## References

- [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html)
- [Windows AppData Folders](https://learn.microsoft.com/en-us/windows/win32/shell/knownfolderid)
- [Go runtime package](https://pkg.go.dev/runtime)
