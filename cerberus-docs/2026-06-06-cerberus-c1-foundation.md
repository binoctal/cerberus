# Cerberus C1 Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Cerberus C1 Foundation — project skeleton, database layer, project plugin system, LLM client (3 providers + mock), AI Driver (budget + prompt + parser), and CLI commands — producing a compilable, fully tested binary ready for Explorer/Judge/Checker integration (C2a/C3).

**Architecture:** Skeleton + Cherry-pick. New project at `./`, porting proven store/migration patterns from relay-test-harness, building all new modules from scratch. All external systems optional; minimum: LLM API key + target URL. SQLite for MVP storage (C1-C3), migrate to PostgreSQL at C4 for JSONB/pgvector support.

**Tech Stack:** Go 1.25.5, cobra (CLI), modernc.org/sqlite (SQLite, pure Go, no CGo), zap (logging), yaml.v3 (config), uuid, testify (tests). LLM clients use raw HTTP (no SDKs).

**Spec:** `docs/2026-06-06-cerberus-design.md`
**Module:** `github.com/binoctal/cerberus`

---

## File Structure

| File | Responsibility | Source |
|------|---------------|--------|
| `cmd/cerberus/main.go` | CLI entry point + cobra commands | New |
| `internal/config/config.go` | Global config from env vars | New |
| `internal/store/store.go` | PG connection management | Ported |
| `internal/store/migrations.go` | V### migration runner | Ported |
| `internal/store/session.go` | Session CRUD | Adapted |
| `internal/store/trace.go` | Trace CRUD | Adapted |
| `internal/store/verdict.go` | Verdict CRUD | New |
| `internal/store/jsonb.go` | JSON marshal helper | Ported |
| `internal/project/schema.go` | Project config type definitions | New |
| `internal/project/loader.go` | YAML loading + env interpolation | New |
| `internal/project/credentials.go` | Credential resolution (file + env) | New |
| `internal/project/defaults.go` | Zero-config defaults | New |
| `internal/project/model.go` | ProjectModel + maturity scoring | New |
| `internal/llm/client.go` | Client interface + Request/Response types | New |
| `internal/llm/mock.go` | Mock LLM for testing | New |
| `internal/llm/claude.go` | Anthropic Claude provider | New |
| `internal/llm/openai.go` | OpenAI GPT provider | New |
| `internal/llm/gemini.go` | Google Gemini provider | New |
| `internal/ai/driver.go` | AIDriver interface + implementation | New |
| `internal/ai/budget.go` | TokenBudget tracking | New |
| `internal/ai/prompt.go` | Prompt construction pipeline | New |
| `internal/ai/context.go` | ContextEntry assembly | New |
| `internal/ai/parser.go` | Structured output parsing | New |
| `internal/session/lifecycle.go` | Session lifecycle (run/verify/serve) | New |
| `migrations/V001__cerberus.sql` | Core DB schema (no pgvector) | New |
| `go.mod` | Module definition | New |
| `Makefile` | Build/test/lint targets | New |
| `.gitignore` | Ignore build artifacts | New |

---

## Task 1: Project Skeleton

**Files:**
- Create: `./go.mod`
- Create: `./Makefile`
- Create: `./.gitignore`
- Create: `./cmd/cerberus/main.go`

- [ ] **Step 1: Create directory structure**

```bash
mkdir -p ./{cmd/cerberus,internal/{config,store,project,llm,ai,session},migrations}
```

- [ ] **Step 2: Create go.mod**

```go
// ./go.mod
module github.com/binoctal/cerberus

go 1.25.5

require (
	github.com/google/uuid v1.6.0
	github.com/spf13/cobra v1.9.1
	modernc.org/sqlite v1.34.5
	github.com/stretchr/testify v1.11.1
	go.uber.org/zap v1.28.0
	gopkg.in/yaml.v3 v3.0.1
)
```

Run: `cd projects/cerberus && go mod tidy`

- [ ] **Step 3: Create Makefile**

```makefile
# ./Makefile
.PHONY: build test lint fmt check clean run

build:
	go build -o bin/cerberus ./cmd/cerberus

test:
	go test -v -race -count=1 ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w -local github.com/binoctal/cerberus .

check: fmt lint test

clean:
	rm -rf bin/

run: build
	./bin/cerberus
```

- [ ] **Step 4: Create .gitignore**

```
# ./.gitignore
bin/
*.exe
```

- [ ] **Step 5: Create main.go stub**

```go
// ./cmd/cerberus/main.go
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Cerberus — Universal AI Testing Framework")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  init    Initialize project configuration")
		fmt.Println("  run     Run intelligent tests")
		fmt.Println("  verify  Verify against known model")
		fmt.Println("  serve   Start HTTP API server")
		fmt.Println()
		fmt.Println("Use \"cerberus <command> --help\" for more information.")
		os.Exit(0)
	}
	// Cobra commands will replace this in Task 11
	fmt.Printf("cerberus %s: not yet implemented\n", os.Args[1])
	os.Exit(1)
}
```

- [ ] **Step 6: Verify build**

Run: `cd projects/cerberus && go build -o bin/cerberus ./cmd/cerberus && ./bin/cerberus`

Expected: prints usage text.

- [ ] **Step 7: Commit**

```bash
git add ./
git commit -m "feat(cerberus): project skeleton with go.mod, Makefile, main.go stub"
```

---

## Task 2: Config Package

**Files:**
- Create: `./internal/config/config.go`
- Create: `./internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
// ./internal/config/config_test.go
package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad(t *testing.T) {
	os.Setenv("CERBERUS_PORT", "9090")
	defer os.Unsetenv("CERBERUS_PORT")

	cfg := Load()
	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, "cerberus.db", cfg.DBPath) // default
}

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{
		"CERBERUS_PORT", "CERBERUS_DB_PATH",
		"CERBERUS_MIGRATION_DIR", "CERBERUS_LOG_LEVEL", "CERBERUS_LLM_MODEL",
	} {
		os.Unsetenv(key)
	}

	cfg := Load()
	assert.Equal(t, "8090", cfg.Port)
	assert.Equal(t, "cerberus.db", cfg.DBPath)
	assert.Equal(t, "migrations", cfg.MigrationDir)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "claude-sonnet-4-6", cfg.LLMModel)
}

func TestDBPath(t *testing.T) {
	cfg := &Config{DBPath: "/tmp/test.db"}
	assert.Equal(t, "/tmp/test.db", cfg.DBPath)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/config/...`
Expected: FAIL — `Config` and `Load` undefined.

- [ ] **Step 3: Implement config.go**

```go
// ./internal/config/config.go
package config

import "os"

type Config struct {
	Port         string
	DBPath       string   // SQLite file path (default: "cerberus.db", use ":memory:" for tests)
	MigrationDir string
	LogLevel     string
	LLMModel     string
	LLMAPIKey    string
}

func Load() *Config {
	return &Config{
		Port:         getEnv("CERBERUS_PORT", "8090"),
		DBPath:       getEnv("CERBERUS_DB_PATH", "cerberus.db"),
		MigrationDir: getEnv("CERBERUS_MIGRATION_DIR", "migrations"),
		LogLevel:     getEnv("CERBERUS_LOG_LEVEL", "info"),
		LLMModel:     getEnv("CERBERUS_LLM_MODEL", "claude-sonnet-4-6"),
		LLMAPIKey:    getEnv("CERBERUS_LLM_API_KEY", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/config/...`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add ./internal/config/
git commit -m "feat(cerberus): config package with env var loading"
```

---

## Task 3: DB Schema + Migration Runner

**Files:**
- Create: `./migrations/V001__cerberus.sql`
- Create: `./internal/store/migrations.go`
- Create: `./internal/store/migrations_test.go`
- Create: `./internal/store/store.go`
- Create: `./internal/store/jsonb.go`

- [ ] **Step 1: Create V001 migration**

```sql
-- ./migrations/V001__cerberus.sql
-- Cerberus core schema. SQLite for MVP (C1-C3).
-- C4 will migrate to PostgreSQL for JSONB, pgvector, and GIN indexes.
-- Foreign keys enforced via PRAGMA in store.go.

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL CHECK (mode IN ('run', 'verify', 'serve')),
  status TEXT NOT NULL DEFAULT 'running',
  goal TEXT,
  project_name TEXT,
  coverage_pct REAL NOT NULL DEFAULT 0,
  stats TEXT NOT NULL DEFAULT '{}',
  started_at TEXT NOT NULL DEFAULT (datetime('now')),
  finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_name);

-- Traces
CREATE TABLE IF NOT EXISTS traces (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  category TEXT NOT NULL,
  target TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'running',
  started_at TEXT NOT NULL DEFAULT (datetime('now')),
  finished_at TEXT,
  metadata TEXT
);
CREATE INDEX IF NOT EXISTS idx_traces_session ON traces(session_id);

-- Evidence
CREATE TABLE IF NOT EXISTS evidence (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  trace_id INTEGER NOT NULL REFERENCES traces(id),
  type TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_evidence_trace ON evidence(trace_id);

-- Verdicts
CREATE TABLE IF NOT EXISTS verdicts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  trace_id INTEGER NOT NULL REFERENCES traces(id),
  target TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pass', 'fail', 'uncertain', 'skip')),
  confidence REAL NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('judge', 'checker', 'merged')),
  reasoning TEXT,
  suggestions TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_verdicts_session ON verdicts(session_id);
CREATE INDEX IF NOT EXISTS idx_verdicts_status ON verdicts(status);

-- L1 Episodic Memory
CREATE TABLE IF NOT EXISTS memory_episodic (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  target TEXT NOT NULL,
  status TEXT NOT NULL,
  verdict TEXT,
  duration_ms INTEGER,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_episodic_target ON memory_episodic(target);
CREATE INDEX IF NOT EXISTS idx_episodic_created ON memory_episodic(created_at DESC);

-- L2 Semantic Memory (embedding + pgvector added in V002 for C4)
CREATE TABLE IF NOT EXISTS memory_semantic (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content TEXT NOT NULL,
  source TEXT NOT NULL,
  tags TEXT NOT NULL DEFAULT '[]',
  confidence REAL NOT NULL DEFAULT 0.5,
  project_name TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- L3 Procedural Memory
CREATE TABLE IF NOT EXISTS memory_procedural (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  condition TEXT NOT NULL,
  action TEXT NOT NULL,
  effectiveness REAL NOT NULL DEFAULT 0.5,
  usage_count INTEGER NOT NULL DEFAULT 0,
  project_name TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Project Models
CREATE TABLE IF NOT EXISTS project_models (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_name TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  model TEXT NOT NULL,
  schema_version INTEGER NOT NULL DEFAULT 1,
  source TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE(project_name, version)
);
```

- [ ] **Step 2: Create store.go (ported from relay-test-harness)**

```go
// ./internal/store/store.go
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

// New opens a SQLite database. Use ":memory:" for in-memory testing.
func New(dbPath string) (*Store, error) {
	dsn := dbPath
	if dbPath != ":memory:" {
		dsn = dbPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	// Enable foreign keys for in-memory databases too
	if dbPath == ":memory:" {
		if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
			return nil, fmt.Errorf("enable foreign keys: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }
```

- [ ] **Step 3: Create jsonb.go (ported)**

```go
// ./internal/store/jsonb.go
package store

import "encoding/json"

// jsonText marshals a value to JSON text for SQLite TEXT columns.
// Returns nil for nil input (stored as NULL in SQLite).
func jsonText(v any) *string {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}
```

- [ ] **Step 4: Create migrations.go (ported from relay-test-harness)**

```go
// ./internal/store/migrations.go
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"sort"
)

var migrationRE = regexp.MustCompile(`^V(\d+)_(.+)\.sql$`)

type migration struct {
	version  int
	filename string
}

func RunMigrations(ctx context.Context, db *sql.DB, dir string) error {
	// Create tracking table
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	// Read migration files
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var migrations []migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		matches := migrationRE.FindStringSubmatch(e.Name())
		if matches == nil {
			continue
		}
		var ver int
		fmt.Sscanf(matches[1], "%d", &ver)
		migrations = append(migrations, migration{version: ver, filename: e.Name()})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	for _, m := range migrations {
		var exists bool
		err := db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", m.version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", m.version, err)
		}
		if exists {
			continue
		}

		content, err := os.ReadFile(dir + "/" + m.filename)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.filename, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for migration %d: %w", m.version, err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", m.filename, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version, name) VALUES (?, ?)",
			m.version, m.filename); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
	}

	return nil
}
```

- [ ] **Step 5: Write migration test**

```go
// ./internal/store/migrations_test.go
package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunMigrations(t *testing.T) {
	s, cleanup := testStore(t)
	defer cleanup()

	ctx := context.Background()
	err := RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	// Verify tables exist (SQLite uses sqlite_master instead of information_schema)
	var count int
	err = s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 7, "expected at least 7 tables")

	// Idempotent
	err = RunMigrations(ctx, s.DB(), "../../migrations")
	assert.NoError(t, err)
}

// testStore creates an in-memory SQLite database for testing.
func testStore(t *testing.T) (*Store, func()) {
	t.Helper()
	s, err := New(":memory:")
	require.NoError(t, err)
	return s, func() { s.Close() }
}

// testStoreWithMigrations creates an in-memory SQLite DB and runs all migrations.
func testStoreWithMigrations(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	require.NoError(t, err)
	err = RunMigrations(context.Background(), s.DB(), "../../migrations")
	require.NoError(t, err, "run migrations")
	return s
}
```

- [ ] **Step 6: Verify build compiles**

Run: `cd projects/cerberus && go build ./...`
Expected: no errors.

Tests use in-memory SQLite — no external database needed.

- [ ] **Step 7: Commit**

```bash
git add ./migrations/ ./internal/store/
git commit -m "feat(cerberus): DB schema V001 + migration runner + store core"
```

---

## Task 4: Store — Sessions + Traces + Verdicts

**Files:**
- Create: `./internal/store/session.go`
- Create: `./internal/store/trace.go`
- Create: `./internal/store/verdict.go`
- Create: `./internal/store/store_test.go`

- [ ] **Step 1: Write session store test**

```go
// ./internal/store/store_test.go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionCRUD(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer s.Close()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "test all APIs", "my-project")
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID)
	assert.Equal(t, "running", sess.Status)

	got, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, got.ID)

	err = s.UpdateSessionStatus(ctx, sess.ID, "completed")
	require.NoError(t, err)
	got, err = s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", got.Status)
	assert.NotEmpty(t, got.FinishedAt)

	sessions, err := s.ListSessions(ctx, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(sessions), 1)
}

func TestTraceCRUD(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer s.Close()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "trace test", "")
	require.NoError(t, err)

	traceID, err := s.CreateTrace(ctx, sess.ID, "api", "GET /api/v1/users")
	require.NoError(t, err)
	assert.Greater(t, traceID, int64(0))

	err = s.FinishTrace(ctx, traceID, "pass")
	require.NoError(t, err)

	traces, err := s.GetTraces(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, traces, 1)
	assert.Equal(t, "pass", traces[0].Status)
}

func TestVerdictCRUD(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer s.Close()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "verdict test", "")
	require.NoError(t, err)
	traceID, err := s.CreateTrace(ctx, sess.ID, "api", "POST /api/v1/users")
	require.NoError(t, err)

	v, err := s.CreateVerdict(ctx, sess.ID, traceID, "POST /api/v1/users",
		"pass", 0.95, "judge", "Response matches expected schema", nil)
	require.NoError(t, err)
	assert.Greater(t, v.ID, int64(0))

	verdicts, err := s.GetVerdicts(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, verdicts, 1)
	assert.Equal(t, "judge", verdicts[0].Source)
}

func TestEpisodicMemory(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer s.Close()
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "memory test", "")
	require.NoError(t, err)

	err = s.RecordEpisodic(ctx, sess.ID, "GET /api/v1/users", "pass",
		map[string]any{"status_code": 200}, 2*time.Second)
	require.NoError(t, err)

	memories, err := s.GetEpisodicByTarget(ctx, "GET /api/v1/users", 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(memories), 1)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd projects/cerberus && go test -v -short -count=1 ./internal/store/...`
Expected: compile errors — missing methods.

- [ ] **Step 3: Implement session.go**

```go
// ./internal/store/session.go
package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID          string `json:"id"`
	Mode        string `json:"mode"`
	Status      string `json:"status"`
	Goal        string `json:"goal,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
	CoveragePct float64 `json:"coverage_pct"`
	Stats       string `json:"stats"`
	StartedAt   string `json:"started_at"`
	FinishedAt  string `json:"finished_at,omitempty"`
}

func (s *Store) CreateSession(ctx context.Context, mode, goal, projectName string) (*Session, error) {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, mode, goal, project_name, started_at) VALUES (?, ?, ?, ?, ?)`,
		id, mode, goal, projectName, now)
	if err != nil {
		return nil, err
	}
	return &Session{
		ID: id, Mode: mode, Status: "running",
		Goal: goal, ProjectName: projectName,
		StartedAt: now, Stats: "{}",
	}, nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	sess := &Session{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, mode, status, goal, project_name, coverage_pct, stats, started_at, COALESCE(finished_at, '')
		 FROM sessions WHERE id = ?`, id).Scan(
		&sess.ID, &sess.Mode, &sess.Status, &sess.Goal, &sess.ProjectName,
		&sess.CoveragePct, &sess.Stats, &sess.StartedAt, &sess.FinishedAt)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, mode, status, goal, project_name, coverage_pct, stats, started_at, COALESCE(finished_at, '')
		 FROM sessions ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.Mode, &sess.Status, &sess.Goal,
			&sess.ProjectName, &sess.CoveragePct, &sess.Stats,
			&sess.StartedAt, &sess.FinishedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) UpdateSessionStatus(ctx context.Context, id string, status string) error {
	isTerminal := false
	for _, t := range []string{"completed", "failed", "aborted"} {
		if status == t {
			isTerminal = true
		}
	}
	if isTerminal {
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET status = ?, finished_at = ? WHERE id = ?`,
			status, now, id)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET status = ? WHERE id = ?`, status, id)
	return err
}
```

- [ ] **Step 4: Implement trace.go**

```go
// ./internal/store/trace.go
package store

import (
	"context"
	"time"
)

type Trace struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"session_id"`
	Category   string `json:"category"`
	Target     string `json:"target"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
	Metadata   string `json:"metadata,omitempty"`
}

func (s *Store) CreateTrace(ctx context.Context, sessionID string, category, target string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO traces (session_id, category, target, started_at) VALUES (?, ?, ?, ?)`,
		sessionID, category, target, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishTrace(ctx context.Context, id int64, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE traces SET status = ?, finished_at = ? WHERE id = ?`,
		status, now, id)
	return err
}

func (s *Store) GetTraces(ctx context.Context, sessionID string) ([]Trace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, category, target, status, started_at, COALESCE(finished_at, ''), COALESCE(metadata, '')
		 FROM traces WHERE session_id = ? ORDER BY started_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var traces []Trace
	for rows.Next() {
		var tr Trace
		if err := rows.Scan(&tr.ID, &tr.SessionID, &tr.Category, &tr.Target,
			&tr.Status, &tr.StartedAt, &tr.FinishedAt, &tr.Metadata); err != nil {
			return nil, err
		}
		traces = append(traces, tr)
	}
	return traces, rows.Err()
}
```

- [ ] **Step 5: Implement verdict.go**

```go
// ./internal/store/verdict.go
package store

import (
	"context"
	"time"
)

type Verdict struct {
	ID          int64   `json:"id"`
	SessionID   string  `json:"session_id"`
	TraceID     int64   `json:"trace_id"`
	Target      string  `json:"target"`
	Status      string  `json:"status"`
	Confidence  float64 `json:"confidence"`
	Source      string  `json:"source"`
	Reasoning   string  `json:"reasoning,omitempty"`
	Suggestions string  `json:"suggestions,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func (s *Store) CreateVerdict(ctx context.Context, sessionID string, traceID int64,
	target, status string, confidence float64, source, reasoning string, suggestions any) (*Verdict, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO verdicts (session_id, trace_id, target, status, confidence, source, reasoning, suggestions, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, traceID, target, status, confidence, source, reasoning, jsonText(suggestions), now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Verdict{
		ID: id, SessionID: sessionID, TraceID: traceID,
		Target: target, Status: status, Confidence: confidence,
		Source: source, Reasoning: reasoning, CreatedAt: now,
	}, nil
}

func (s *Store) GetVerdicts(ctx context.Context, sessionID string) ([]Verdict, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, trace_id, target, status, confidence, source,
		        COALESCE(reasoning, ''), COALESCE(suggestions, ''), created_at
		 FROM verdicts WHERE session_id = ? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var verdicts []Verdict
	for rows.Next() {
		var v Verdict
		if err := rows.Scan(&v.ID, &v.SessionID, &v.TraceID, &v.Target,
			&v.Status, &v.Confidence, &v.Source, &v.Reasoning, &v.Suggestions,
			&v.CreatedAt); err != nil {
			return nil, err
		}
		verdicts = append(verdicts, v)
	}
	return verdicts, rows.Err()
}
```

- [ ] **Step 6: Add episodic memory methods to verdict.go (or a separate file)**

Create `./internal/store/memory.go`:

```go
// ./internal/store/memory.go
package store

import (
	"context"
	"time"
)

type EpisodicMemory struct {
	ID        int64  `json:"id"`
	SessionID string `json:"session_id"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	Verdict   string `json:"verdict,omitempty"`
	DurationMs int64 `json:"duration_ms,omitempty"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) RecordEpisodic(ctx context.Context, sessionID string,
	target, status string, verdict any, duration time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_episodic (session_id, target, status, verdict, duration_ms)
		 VALUES (?, ?, ?, ?, ?)`,
		sessionID, target, status, jsonText(verdict), duration.Milliseconds())
	return err
}

func (s *Store) GetEpisodicByTarget(ctx context.Context, target string, limit int) ([]EpisodicMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, target, status, COALESCE(verdict, ''), COALESCE(duration_ms, 0), created_at
		 FROM memory_episodic WHERE target = ? ORDER BY created_at DESC LIMIT ?`,
		target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []EpisodicMemory
	for rows.Next() {
		var m EpisodicMemory
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Target, &m.Status,
			&m.Verdict, &m.DurationMs, &m.CreatedAt); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}
	return memories, rows.Err()
}
```

- [ ] **Step 7: Verify build + run short tests**

Run: `go build ./... && go test -v -count=1 ./internal/store/...`
Expected: all tests pass (in-memory SQLite, no external DB needed).

- [ ] **Step 8: Commit**

```bash
git add ./internal/store/
git commit -m "feat(cerberus): store sessions, traces, verdicts, episodic memory"
```

---

## Task 5: Project Plugin — Schema Types

**Files:**
- Create: `./internal/project/schema.go`
- Create: `./internal/project/schema_test.go`

- [ ] **Step 1: Write the failing test**

```go
// ./internal/project/schema_test.go
package project

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestProjectYAMLParse(t *testing.T) {
	input := `
project:
  name: my-saas
services:
  - name: web
    url: "http://localhost:3000"
    health: "/"
  - name: api
    url: "http://localhost:8080"
actors:
  - name: admin
    credentials:
      email: "${ADMIN_EMAIL}"
      password: "${ADMIN_PASS}"
    entry: "/admin"
databases:
  - name: main
    url: "${DATABASE_URL}"
invariants:
  - id: INV-001
    description: "balance cannot be negative"
    severity: critical
    check: "SELECT COUNT(*) AS cnt FROM users WHERE balance < 0"
    assertion: "cnt == 0"
settings:
  max_duration: 30m
  confidence_threshold: 0.7
  auto_fix: low_only
`
	var cfg Config
	err := yaml.Unmarshal([]byte(input), &cfg)
	require.NoError(t, err)

	assert.Equal(t, "my-saas", cfg.Project.Name)
	require.Len(t, cfg.Services, 2)
	assert.Equal(t, "http://localhost:3000", cfg.Services[0].URL)
	require.Len(t, cfg.Actors, 1)
	assert.Equal(t, "${ADMIN_EMAIL}", cfg.Actors[0].Credentials.Email)
	require.Len(t, cfg.Databases, 1)
	require.Len(t, cfg.Invariants, 1)
	assert.Equal(t, "cnt == 0", cfg.Invariants[0].Assertion)
	assert.Equal(t, 0.7, cfg.Settings.ConfidenceThreshold)
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 200000, cfg.Settings.AIBudget.SessionTotalTokens)
	assert.Equal(t, 10000, cfg.Settings.AIBudget.PerCallLimit)
	assert.Equal(t, 0.7, cfg.Settings.ConfidenceThreshold)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/project/...`
Expected: FAIL — types undefined.

- [ ] **Step 3: Implement schema.go**

```go
// ./internal/project/schema.go
package project

// Config is the top-level project configuration.
// All fields are optional — zero-config runs with just --url and --goal.
type Config struct {
	Project    ProjectMeta `yaml:"project"`
	Services   []Service   `yaml:"services,omitempty"`
	Actors     []Actor     `yaml:"actors,omitempty"`
	Databases  []Database  `yaml:"databases,omitempty"`
	Code       CodeConfig  `yaml:"code,omitempty"`
	Invariants []Invariant `yaml:"invariants,omitempty"`
	Settings   Settings    `yaml:"settings,omitempty"`
}

type ProjectMeta struct {
	Name string `yaml:"name"`
}

type Service struct {
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	Health string `yaml:"health,omitempty"`
}

type Actor struct {
	Name        string       `yaml:"name"`
	Credentials CredentialRef `yaml:"credentials"`
	Entry       string       `yaml:"entry,omitempty"`
}

type CredentialRef struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
}

type Database struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

type CodeConfig struct {
	Root      string   `yaml:"root,omitempty"`
	Providers []string `yaml:"providers,omitempty"`
}

type Invariant struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Severity    string `yaml:"severity,omitempty"`
	Check       string `yaml:"check"`
	Assertion   string `yaml:"assertion,omitempty"`
}

type Settings struct {
	MaxDuration         string   `yaml:"max_duration,omitempty"`
	ConfidenceThreshold float64  `yaml:"confidence_threshold,omitempty"`
	AutoFix             string   `yaml:"auto_fix,omitempty"`
	AIBudget            AIBudget `yaml:"ai_budget,omitempty"`
	CostAlerts          CostAlerts `yaml:"cost_alerts,omitempty"`
}

type AIBudget struct {
	SessionTotalTokens int    `yaml:"session_total_tokens,omitempty"`
	PerCallLimit       int    `yaml:"per_call_limit,omitempty"`
	Model              string `yaml:"model,omitempty"`
}

type CostAlerts struct {
	WarnAtPct int `yaml:"warn_at_pct,omitempty"`
	StopAtPct int `yaml:"stop_at_pct,omitempty"`
}
```

- [ ] **Step 4: Implement defaults.go**

```go
// ./internal/project/defaults.go
package project

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Settings: Settings{
			MaxDuration:         "30m",
			ConfidenceThreshold: 0.7,
			AutoFix:             "low_only",
			AIBudget: AIBudget{
				SessionTotalTokens: 200000,
				PerCallLimit:       10000,
				Model:              "claude-sonnet-4-6",
			},
			CostAlerts: CostAlerts{
				WarnAtPct: 80,
				StopAtPct: 100,
			},
		},
	}
}
```

- [ ] **Step 5: Run tests**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/project/...`
Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```bash
git add ./internal/project/
git commit -m "feat(cerberus): project plugin schema types with defaults"
```

---

## Task 6: Project Plugin — Loader + Credentials + Model

**Files:**
- Create: `./internal/project/loader.go`
- Create: `./internal/project/credentials.go`
- Create: `./internal/project/model.go`
- Modify: `./internal/project/schema_test.go`

- [ ] **Step 1: Write failing tests for loader + credentials**

Append to `./internal/project/schema_test.go`:

```go
func TestLoaderEnvInterpolation(t *testing.T) {
	os.Setenv("ADMIN_EMAIL", "admin@test.dev")
	os.Setenv("ADMIN_PASS", "secret123")
	os.Setenv("DATABASE_URL", "postgres://localhost/mydb")
	defer func() {
		os.Unsetenv("ADMIN_EMAIL")
		os.Unsetenv("ADMIN_PASS")
		os.Unsetenv("DATABASE_URL")
	}()

	input := `
project:
  name: test
actors:
  - name: admin
    credentials:
      email: "${ADMIN_EMAIL}"
      password: "${ADMIN_PASS}"
databases:
  - name: main
    url: "${DATABASE_URL}"
`
	cfg, err := LoadFromYAML([]byte(input))
	require.NoError(t, err)
	assert.Equal(t, "admin@test.dev", cfg.Actors[0].Credentials.Email)
	assert.Equal(t, "secret123", cfg.Actors[0].Credentials.Password)
	assert.Equal(t, "postgres://localhost/mydb", cfg.Databases[0].URL)
}

func TestCredentialResolution(t *testing.T) {
	// Test env var override: CERBERUS_ACTOR_ADMIN_EMAIL / CERBERUS_ACTOR_ADMIN_PASSWORD
	os.Setenv("CERBERUS_ACTOR_ADMIN_EMAIL", "env-admin@test.dev")
	os.Setenv("CERBERUS_ACTOR_ADMIN_PASSWORD", "env-secret")
	defer func() {
		os.Unsetenv("CERBERUS_ACTOR_ADMIN_EMAIL")
		os.Unsetenv("CERBERUS_ACTOR_ADMIN_PASSWORD")
	}()

	cfg := &Config{
		Actors: []Actor{
			{Name: "admin", Credentials: CredentialRef{
				Email: "file-admin@test.dev", Password: "file-secret",
			}},
		},
	}
	resolved := ResolveCredentials(cfg)
	assert.Equal(t, "env-admin@test.dev", resolved.Actors[0].Credentials.Email)
	assert.Equal(t, "env-secret", resolved.Actors[0].Credentials.Password)
}

func TestCredentialFileFallback(t *testing.T) {
	// No env vars — use file values
	cfg := &Config{
		Actors: []Actor{
			{Name: "admin", Credentials: CredentialRef{
				Email: "file-admin@test.dev", Password: "file-secret",
			}},
		},
	}
	resolved := ResolveCredentials(cfg)
	assert.Equal(t, "file-admin@test.dev", resolved.Actors[0].Credentials.Email)
}

func TestProjectModelMaturity(t *testing.T) {
	pm := &ProjectModel{
		Navigation: NavigationModel{Pages: []PageDef{
			{Path: "/"}, {Path: "/login"}, {Path: "/admin"},
		}, TotalPages: 10},
		API: APIModel{Endpoints: []EndpointDef{
			{Method: "GET", Path: "/api/v1/users"},
		}, TotalEndpoints: 5},
		SchemaAnalyzed: false,
	}
	score := pm.MaturityScore()
	assert.InDelta(t, 0.17, score, 0.01) // (3/10)*0.3 + (1/5)*0.4 + 0*0.3 = 0.09+0.08+0 = 0.17

	pm.SchemaAnalyzed = true
	score = pm.MaturityScore()
	assert.Greater(t, score, 0.3)
}
```

Add `"os"` to the imports.

- [ ] **Step 2: Run to verify it fails**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/project/...`
Expected: FAIL — `LoadFromYAML`, `ResolveCredentials`, `ProjectModel` undefined.

- [ ] **Step 3: Implement loader.go**

```go
// ./internal/project/loader.go
package project

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

var envVarRE = regexp.MustCompile(`\$\{([^}]+)\}`)

// LoadFromYAML parses YAML bytes with environment variable interpolation.
func LoadFromYAML(data []byte) (*Config, error) {
	// Interpolate ${VAR} references
	interpolated := envVarRE.ReplaceAllFunc(data, func(match []byte) []byte {
		varName := string(match[2 : len(match)-1]) // strip ${ and }
		if val := os.Getenv(varName); val != "" {
			return []byte(val)
		}
		return match // leave as-is if not found
	})

	var cfg Config
	if err := yaml.Unmarshal(interpolated, &cfg); err != nil {
		return nil, fmt.Errorf("parse project config: %w", err)
	}

	// Apply defaults for zero-value fields
	applyDefaults(&cfg)
	return &cfg, nil
}

// LoadFromFile reads a YAML file and parses it.
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read project config: %w", err)
	}
	return LoadFromYAML(data)
}

func applyDefaults(cfg *Config) {
	d := DefaultConfig()
	if cfg.Settings.ConfidenceThreshold == 0 {
		cfg.Settings.ConfidenceThreshold = d.Settings.ConfidenceThreshold
	}
	if cfg.Settings.MaxDuration == "" {
		cfg.Settings.MaxDuration = d.Settings.MaxDuration
	}
	if cfg.Settings.AutoFix == "" {
		cfg.Settings.AutoFix = d.Settings.AutoFix
	}
	if cfg.Settings.AIBudget.SessionTotalTokens == 0 {
		cfg.Settings.AIBudget = d.Settings.AIBudget
	}
	if cfg.Settings.CostAlerts.WarnAtPct == 0 {
		cfg.Settings.CostAlerts = d.Settings.CostAlerts
	}
}
```

- [ ] **Step 4: Implement credentials.go**

```go
// ./internal/project/credentials.go
package project

import (
	"fmt"
	"os"
	"strings"
)

// ResolveCredentials resolves actor credentials from env vars, falling back to file values.
// Env var pattern: CERBERUS_ACTOR_<UPPER_NAME>_EMAIL / CERBERUS_ACTOR_<UPPER_NAME>_PASSWORD
func ResolveCredentials(cfg *Config) *Config {
	result := *cfg // shallow copy
	result.Actors = make([]Actor, len(cfg.Actors))
	for i, a := range cfg.Actors {
		actor := a
		envPrefix := "CERBERUS_ACTOR_" + strings.ToUpper(strings.ReplaceAll(a.Name, "-", "_"))
		if email := os.Getenv(envPrefix + "_EMAIL"); email != "" {
			actor.Credentials.Email = email
		}
		if pass := os.Getenv(envPrefix + "_PASSWORD"); pass != "" {
			actor.Credentials.Password = pass
		}
		result.Actors[i] = actor
	}
	return &result
}

// ResolveActorCredentials returns resolved credentials for a specific actor by name.
func ResolveActorCredentials(cfg *Config, actorName string) (email, password string, err error) {
	resolved := ResolveCredentials(cfg)
	for _, a := range resolved.Actors {
		if a.Name == actorName {
			return a.Credentials.Email, a.Credentials.Password, nil
		}
	}
	return "", "", fmt.Errorf("actor %q not found in config", actorName)
}
```

- [ ] **Step 5: Implement model.go**

```go
// ./internal/project/model.go
package project

// ProjectModel is the cognitive model built during exploration (§3.2).
// Stored in project_models table and .cerberus/project-model.yaml.
type ProjectModel struct {
	Navigation    NavigationModel `yaml:"navigation"`
	API           APIModel        `yaml:"api"`
	SchemaAnalyzed bool           `yaml:"schema_analyzed"`
	TechStack     []string        `yaml:"tech_stack,omitempty"`
	InvariantHints []InvariantHint `yaml:"invariants_hints,omitempty"`
}

type NavigationModel struct {
	Pages      []PageDef `yaml:"pages"`
	TotalPages int       `yaml:"total_pages"`
}

type PageDef struct {
	Path        string  `yaml:"path"`
	Type        string  `yaml:"type,omitempty"`
	RequiresAuth bool   `yaml:"requires_auth,omitempty"`
	Confidence  float64 `yaml:"confidence"`
}

type APIModel struct {
	Endpoints      []EndpointDef `yaml:"endpoints"`
	TotalEndpoints int           `yaml:"total_endpoints"`
}

type EndpointDef struct {
	Method     string  `yaml:"method"`
	Path       string  `yaml:"path"`
	Confidence float64 `yaml:"confidence"`
}

type InvariantHint struct {
	ID          string  `yaml:"id"`
	Source      string  `yaml:"source"`
	Description string  `yaml:"description"`
	Confidence  float64 `yaml:"confidence"`
	Severity    string  `yaml:"severity,omitempty"`
}

// MaturityScore computes project model maturity (0.0 - 1.0).
// Controls cognition/test ratio (§3.2.1).
func (pm *ProjectModel) MaturityScore() float64 {
	if pm == nil {
		return 0
	}
	pageScore := 0.0
	if pm.Navigation.TotalPages > 0 {
		pageScore = float64(len(pm.Navigation.Pages)) / float64(pm.Navigation.TotalPages)
	}

	apiScore := 0.0
	if pm.API.TotalEndpoints > 0 {
		apiScore = float64(len(pm.API.Endpoints)) / float64(pm.API.TotalEndpoints)
	}

	schemaScore := 0.0
	if pm.SchemaAnalyzed {
		schemaScore = 1.0
	}

	return pageScore*0.3 + apiScore*0.4 + schemaScore*0.3
}
```

- [ ] **Step 6: Run tests**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/project/...`
Expected: PASS (6 tests).

- [ ] **Step 7: Commit**

```bash
git add ./internal/project/
git commit -m "feat(cerberus): project loader, credentials, model with maturity scoring"
```

---

## Task 7: LLM Client — Interface + Mock

**Files:**
- Create: `./internal/llm/client.go`
- Create: `./internal/llm/mock.go`
- Create: `./internal/llm/client_test.go`

- [ ] **Step 1: Write the failing test**

```go
// ./internal/llm/client_test.go
package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockClient(t *testing.T) {
	mock := NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9}`,
	})

	resp, err := mock.Complete(context.Background(), Request{
		Model:    "mock",
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)
	assert.Equal(t, `{"status":"pass","confidence":0.9}`, resp.Content)
	assert.Greater(t, resp.Usage.TotalTokens, 0)
}

func TestMockClientWithVision(t *testing.T) {
	mock := NewMockClient(map[string]string{
		"default": `{"status":"pass"}`,
	})

	resp, err := mock.CompleteWithVision(context.Background(),
		"describe this", [][]byte{{1, 2, 3}})
	require.NoError(t, err)
	assert.Equal(t, `{"status":"pass"}`, resp.Content)
}

func TestAutoDetectProvider(t *testing.T) {
	tests := []struct {
		model    string
		provider string
	}{
		{"claude-sonnet-4-6", "anthropic"},
		{"claude-opus-4-8", "anthropic"},
		{"gpt-4.1-2025-04-14", "openai"},
		{"gpt-4o-mini", "openai"},
		{"gemini-3-flash-preview", "gemini"},
		{"gemini-2.5-pro", "gemini"},
		{"mock", "mock"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.provider, detectProvider(tt.model))
		})
	}
}

func TestNewClientAutoDetection(t *testing.T) {
	// With mock model, should get MockClient
	client, err := NewClient("mock", "")
	require.NoError(t, err)
	assert.NotNil(t, client)
	_, ok := client.(*MockClient)
	assert.True(t, ok)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/llm/...`
Expected: FAIL — types undefined.

- [ ] **Step 3: Implement client.go**

```go
// ./internal/llm/client.go
package llm

import (
	"context"
	"fmt"
	"strings"
)

type Client interface {
	Complete(ctx context.Context, req Request) (*Response, error)
	CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error)
}

type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	MaxTokens int      `json:"max_tokens,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Response struct {
	Content    string     `json:"content"`
	Usage      TokenUsage `json:"usage"`
	StopReason string     `json:"stop_reason,omitempty"`
}

type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// NewClient creates a client based on model name prefix.
func NewClient(model, apiKey string) (Client, error) {
	provider := detectProvider(model)
	switch provider {
	case "anthropic":
		return NewClaudeClient(apiKey, model), nil
	case "openai":
		return NewOpenAIClient(apiKey, model), nil
	case "gemini":
		return NewGeminiClient(apiKey, model), nil
	case "mock":
		return NewMockClient(map[string]string{
			"default": `{"status":"pass","confidence":0.9,"reasoning":"mock response"}`,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported model: %s", model)
	}
}

func detectProvider(model string) string {
	if model == "mock" {
		return "mock"
	}
	if strings.HasPrefix(model, "claude") {
		return "anthropic"
	}
	if strings.HasPrefix(model, "gpt") {
		return "openai"
	}
	if strings.HasPrefix(model, "gemini") {
		return "gemini"
	}
	return ""
}
```

- [ ] **Step 4: Implement mock.go**

```go
// ./internal/llm/mock.go
package llm

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
)

// MockClient returns pre-configured responses. Used for testing without API keys.
type MockClient struct {
	responses map[string]string
}

func NewMockClient(responses map[string]string) *MockClient {
	return &MockClient{responses: responses}
}

func (m *MockClient) Complete(ctx context.Context, req Request) (*Response, error) {
	key := m.matchKey(strings.Join(func() []string {
		var contents []string
		for _, msg := range req.Messages {
			contents = append(contents, msg.Content)
		}
		return contents
	}(), " "))

	content, ok := m.responses[key]
	if !ok {
		content = m.responses["default"]
	}

	inputTokens := len(req.Messages) * 10 // rough estimate
	outputTokens := len(content) / 4
	return &Response{
		Content:    content,
		StopReason: "end_turn",
		Usage: TokenUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
		},
	}, nil
}

func (m *MockClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error) {
	resp, err := m.Complete(ctx, Request{
		Messages: []Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return nil, err
	}
	resp.Usage.InputTokens += len(images) * 100 // vision token estimate
	resp.Usage.TotalTokens = resp.Usage.InputTokens + resp.Usage.OutputTokens
	return resp, nil
}

func (m *MockClient) matchKey(input string) string {
	if _, ok := m.responses[input]; ok {
		return input
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))[:8]
}
```

- [ ] **Step 5: Create stub files for providers (so NewClient compiles)**

```go
// ./internal/llm/claude.go
package llm

type ClaudeClient struct {
	apiKey string
	model  string
}

func NewClaudeClient(apiKey, model string) *ClaudeClient {
	return &ClaudeClient{apiKey: apiKey, model: model}
}

func (c *ClaudeClient) Complete(ctx context.Context, req Request) (*Response, error) {
	// Full implementation in Task 8
	return nil, fmt.Errorf("claude: not yet implemented")
}

func (c *ClaudeClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error) {
	return nil, fmt.Errorf("claude vision: not yet implemented")
}
```

Create similar stubs for `openai.go` and `gemini.go`.

- [ ] **Step 6: Run tests**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/llm/...`
Expected: PASS (4 tests — mock, vision, auto-detect, new client).

- [ ] **Step 7: Commit**

```bash
git add ./internal/llm/
git commit -m "feat(cerberus): LLM client interface, mock client, auto-detection"
```

---

## Task 8: LLM Client — Claude + OpenAI + Gemini

**Files:**
- Modify: `./internal/llm/claude.go`
- Modify: `./internal/llm/openai.go`
- Modify: `./internal/llm/gemini.go`
- Modify: `./internal/llm/client_test.go`

- [ ] **Step 1: Add request construction tests**

Append to `client_test.go`:

```go
func TestClaudeRequestConstruction(t *testing.T) {
	client := NewClaudeClient("test-key", "claude-sonnet-4-6")
	assert.Equal(t, "test-key", client.apiKey)
	assert.Equal(t, "claude-sonnet-4-6", client.model)
	assert.Equal(t, "https://api.anthropic.com/v1/messages", client.baseURL())
}

func TestOpenAIRequestConstruction(t *testing.T) {
	client := NewOpenAIClient("test-key", "gpt-4.1-2025-04-14")
	assert.Equal(t, "test-key", client.apiKey)
	assert.Equal(t, "https://api.openai.com/v1/chat/completions", client.baseURL())
}

func TestGeminiRequestConstruction(t *testing.T) {
	client := NewGeminiClient("test-key", "gemini-3-flash-preview")
	assert.Equal(t, "test-key", client.apiKey)
	assert.Contains(t, client.baseURL(), "generativelanguage.googleapis.com")
}
```

- [ ] **Step 2: Implement claude.go**

```go
// ./internal/llm/claude.go
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type ClaudeClient struct {
	apiKey string
	model  string
}

func NewClaudeClient(apiKey, model string) *ClaudeClient {
	return &ClaudeClient{apiKey: apiKey, model: model}
}

func (c *ClaudeClient) baseURL() string { return "https://api.anthropic.com/v1/messages" }

func (c *ClaudeClient) Complete(ctx context.Context, req Request) (*Response, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body := map[string]any{
		"model":      req.Model,
		"max_tokens": max(req.MaxTokens, 4096),
		"messages":   req.Messages,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic api error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var content string
	if len(result.Content) > 0 {
		content = result.Content[0].Text
	}

	return &Response{
		Content:    content,
		StopReason: result.StopReason,
		Usage: TokenUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.InputTokens + result.Usage.OutputTokens,
		},
	}, nil
}

func (c *ClaudeClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error) {
	content := []map[string]any{
		{"type": "text", "text": prompt},
	}
	for _, img := range images {
		content = append(content, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": "image/png",
				"data":       base64.StdEncoding.EncodeToString(img),
			},
		})
	}

	body := map[string]any{
		"model":      c.model,
		"max_tokens": 4096,
		"messages":   []map[string]any{{"role": "user", "content": content}},
	}
	b, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	var text string
	if len(result.Content) > 0 {
		text = result.Content[0].Text
	}
	return &Response{
		Content:    text,
		StopReason: result.StopReason,
		Usage: TokenUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.InputTokens + result.Usage.OutputTokens,
		},
	}, nil
}

// max is a built-in function in Go 1.21+, no custom implementation needed.
```

- [ ] **Step 3: Implement openai.go**

```go
// ./internal/llm/openai.go
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAIClient struct {
	apiKey string
	model  string
}

func NewOpenAIClient(apiKey, model string) *OpenAIClient {
	return &OpenAIClient{apiKey: apiKey, model: model}
}

func (c *OpenAIClient) baseURL() string { return "https://api.openai.com/v1/chat/completions" }

func (c *OpenAIClient) Complete(ctx context.Context, req Request) (*Response, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body := map[string]any{
		"model":       req.Model,
		"messages":    req.Messages,
		"max_tokens":  max(req.MaxTokens, 4096),
		"temperature": 0.1,
	}
	b, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call openai: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai api error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	var content string
	if len(result.Choices) > 0 {
		content = result.Choices[0].Message.Content
	}
	return &Response{
		Content:    content,
		StopReason: func() string { if len(result.Choices) > 0 { return result.Choices[0].FinishReason }; return "" }(),
		Usage: TokenUsage{
			InputTokens:  result.Usage.PromptTokens,
			OutputTokens: result.Usage.CompletionTokens,
			TotalTokens:  result.Usage.TotalTokens,
		},
	}, nil
}

func (c *OpenAIClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error) {
	content := []map[string]any{
		{"type": "text", "text": prompt},
	}
	for _, img := range images {
		content = append(content, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(img),
			},
		})
	}

	body := map[string]any{
		"model": c.model,
		"messages": []map[string]any{{
			"role":    "user",
			"content": content,
		}},
		"max_tokens": 4096,
	}
	b, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	content := ""
	if len(result.Choices) > 0 {
		content = result.Choices[0].Message.Content
	}
	return &Response{Content: content, Usage: TokenUsage{TotalTokens: result.Usage.TotalTokens}}, nil
}
```

- [ ] **Step 4: Implement gemini.go**

```go
// ./internal/llm/gemini.go
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type GeminiClient struct {
	apiKey string
	model  string
}

func NewGeminiClient(apiKey, model string) *GeminiClient {
	return &GeminiClient{apiKey: apiKey, model: model}
}

func (c *GeminiClient) baseURL() string {
	return fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent",
		c.model)
}

func (c *GeminiClient) Complete(ctx context.Context, req Request) (*Response, error) {
	msgs := make([]map[string]any, len(req.Messages))
	for i, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		msgs[i] = map[string]any{
			"role": role,
			"parts": []map[string]any{
				{"text": m.Content},
			},
		}
	}

	body := map[string]any{
		"contents": msgs,
		"generationConfig": map[string]any{
			"temperature":     1.0,
			"maxOutputTokens": max(req.MaxTokens, 4096),
		},
	}
	b, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call gemini: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini api error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	var content string
	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		content = result.Candidates[0].Content.Parts[0].Text
	}
	return &Response{
		Content:    content,
		StopReason: func() string { if len(result.Candidates) > 0 { return result.Candidates[0].FinishReason }; return "" }(),
		Usage: TokenUsage{
			InputTokens:  result.UsageMetadata.PromptTokenCount,
			OutputTokens: result.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  result.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

func (c *GeminiClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*Response, error) {
	parts := []map[string]any{{"text": prompt}}
	for _, img := range images {
		parts = append(parts, map[string]any{
			"inline_data": map[string]any{
				"mime_type": "image/png",
				"data":      base64.StdEncoding.EncodeToString(img),
			},
		})
	}

	body := map[string]any{
		"contents": []map[string]any{{
			"role":  "user",
			"parts": parts,
		}},
	}
	b, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			TotalTokenCount int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	content := ""
	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		content = result.Candidates[0].Content.Parts[0].Text
	}
	return &Response{
		Content: content,
		Usage:   TokenUsage{TotalTokens: result.UsageMetadata.TotalTokenCount},
	}, nil
}
```

- [ ] **Step 5: Run tests**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/llm/...`
Expected: PASS (7 tests — mock, vision, auto-detect, new client, + 3 construction tests).

- [ ] **Step 6: Commit**

```bash
git add ./internal/llm/
git commit -m "feat(cerberus): LLM client — Claude, OpenAI, Gemini providers"
```

---

## Task 9: AI Driver — Budget + Prompt + Context + Parser + Driver

**Files:**
- Create: `./internal/ai/budget.go`
- Create: `./internal/ai/prompt.go`
- Create: `./internal/ai/context.go`
- Create: `./internal/ai/parser.go`
- Create: `./internal/ai/driver.go`
- Create: `./internal/ai/driver_test.go`

- [ ] **Step 1: Write the failing test**

```go
// ./internal/ai/driver_test.go
package ai

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenBudget(t *testing.T) {
	b := NewTokenBudget(100000, 10000)
	assert.Equal(t, 100000, b.SessionTotal)
	assert.Equal(t, 100000, b.Remaining())

	b.Record(30000)
	assert.Equal(t, 70000, b.Remaining())
	assert.False(t, b.Exhausted())

	b.Record(70000)
	assert.True(t, b.Exhausted())
}

func TestTokenBudgetCanSpend(t *testing.T) {
	b := NewTokenBudget(100000, 10000)
	assert.True(t, b.CanSpend(5000))
	assert.True(t, b.CanSpend(10000))  // exactly per-call limit
	assert.False(t, b.CanSpend(10001)) // exceeds per-call limit
}

func TestPromptBuilder(t *testing.T) {
	prompt := NewPrompt().
		System("You are a test judge.").
		Task("Evaluate this evidence: status code 200").
		Output("JSON with status and confidence fields").
		Build()

	assert.Contains(t, prompt, "You are a test judge.")
	assert.Contains(t, prompt, "Evaluate this evidence")
	assert.Contains(t, prompt, "JSON with status")
}

func TestContextInjection(t *testing.T) {
	entries := []ContextEntry{
		{Source: "memory", Content: "Last test found 500 error", Relevance: 0.9},
		{Source: "code", Content: "Endpoint: POST /api/v1/users", Relevance: 0.8},
	}
	ctx := BuildContext(entries)
	assert.Contains(t, ctx, "Last test found 500 error")
	assert.Contains(t, ctx, "POST /api/v1/users")
}

func TestParseStructuredOutput(t *testing.T) {
	type TestResult struct {
		Status     string  `json:"status"`
		Confidence float64 `json:"confidence"`
	}

	input := `Here is my analysis:
```json
{"status": "pass", "confidence": 0.95}
```
The test passed.`

	var result TestResult
	err := ParseStructuredOutput(input, &result)
	require.NoError(t, err)
	assert.Equal(t, "pass", result.Status)
	assert.InDelta(t, 0.95, result.Confidence, 0.01)
}

func TestParseStructuredOutputDirect(t *testing.T) {
	type Result struct{ Status string `json:"status"` }
	var r Result
	err := ParseStructuredOutput(`{"status":"fail"}`, &r)
	require.NoError(t, err)
	assert.Equal(t, "fail", r.Status)
}

func TestDriverDecide(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"verdict":"pass","confidence":0.9,"reasoning":"looks good"}`,
	})

	driver := NewDriver(mockClient, NewTokenBudget(200000, 10000))

	type Verdict struct {
		Verdict    string  `json:"verdict"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	var v Verdict
	err := driver.Decide(context.Background(),
		NewPrompt().System("judge").Task("evaluate").Output("JSON verdict").Build(),
		&v,
	)
	require.NoError(t, err)
	assert.Equal(t, "pass", v.Verdict)
	assert.Equal(t, "looks good", v.Reasoning)

	// Budget should have been deducted
	assert.Less(t, driver.Budget().Remaining(), 200000)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/ai/...`
Expected: FAIL — types undefined.

- [ ] **Step 3: Implement budget.go**

```go
// ./internal/ai/budget.go
package ai

import "sync/atomic"

type TokenBudget struct {
	SessionTotal int
	PerCallLimit int
	spent        atomic.Int64
}

func NewTokenBudget(sessionTotal, perCallLimit int) *TokenBudget {
	return &TokenBudget{
		SessionTotal: sessionTotal,
		PerCallLimit: perCallLimit,
	}
}

func (b *TokenBudget) Remaining() int {
	return b.SessionTotal - int(b.spent.Load())
}

func (b *TokenBudget) Record(tokens int) {
	b.spent.Add(int64(tokens))
}

func (b *TokenBudget) CanSpend(tokens int) bool {
	if tokens > b.PerCallLimit {
		return false
	}
	return tokens <= b.Remaining()
}

func (b *TokenBudget) Exhausted() bool {
	return b.Remaining() <= 0
}
```

- [ ] **Step 4: Implement prompt.go**

```go
// ./internal/ai/prompt.go
package ai

import "strings"

type promptBuilder struct {
	system   string
	context  string
	task     string
	output   string
}

func NewPrompt() *promptBuilder {
	return &promptBuilder{}
}

func (p *promptBuilder) System(s string) *promptBuilder    { p.system = s; return p }
func (p *promptBuilder) Context(s string) *promptBuilder   { p.context = s; return p }
func (p *promptBuilder) Task(s string) *promptBuilder      { p.task = s; return p }
func (p *promptBuilder) Output(s string) *promptBuilder    { p.output = s; return p }

func (p *promptBuilder) Build() string {
	var parts []string
	if p.system != "" {
		parts = append(parts, p.system)
	}
	if p.context != "" {
		parts = append(parts, "\n## Context\n"+p.context)
	}
	if p.task != "" {
		parts = append(parts, "\n## Task\n"+p.task)
	}
	if p.output != "" {
		parts = append(parts, "\n## Output Format\n"+p.output)
	}
	return strings.Join(parts, "\n")
}
```

- [ ] **Step 5: Implement context.go**

```go
// ./internal/ai/context.go
package ai

import (
	"fmt"
	"sort"
	"strings"
)

type ContextEntry struct {
	Source    string  `json:"source"`
	Content   string  `json:"content"`
	Relevance float64 `json:"relevance"`
}

func BuildContext(entries []ContextEntry) string {
	if len(entries) == 0 {
		return ""
	}

	// Sort by relevance descending
	sorted := make([]ContextEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Relevance > sorted[j].Relevance
	})

	var b strings.Builder
	for _, e := range sorted {
		fmt.Fprintf(&b, "[%s] %s\n", e.Source, e.Content)
	}
	return b.String()
}
```

- [ ] **Step 6: Implement parser.go**

```go
// ./internal/ai/parser.go
package ai

import (
	"encoding/json"
	"regexp"
)

var jsonBlockRE = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(.*?)\\n```")

// ParseStructuredOutput extracts JSON from an LLM response and unmarshals into target.
// Handles both raw JSON and markdown-fenced JSON blocks.
func ParseStructuredOutput(input string, target any) error {
	// Try direct JSON parse first
	if err := json.Unmarshal([]byte(input), target); err == nil {
		return nil
	}

	// Try extracting from markdown code block
	match := jsonBlockRE.FindStringSubmatch(input)
	if match != nil {
		return json.Unmarshal([]byte(match[1]), target)
	}

	// Last resort: find first { to last }
	start := -1
	end := -1
	for i, c := range input {
		if c == '{' && start == -1 {
			start = i
		}
		if c == '}' {
			end = i
		}
	}
	if start != -1 && end > start {
		return json.Unmarshal([]byte(input[start:end+1]), target)
	}

	return json.Unmarshal([]byte(input), target) // return original error
}
```

- [ ] **Step 7: Implement driver.go**

```go
// ./internal/ai/driver.go
package ai

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/llm"
)

type Driver struct {
	client llm.Client
	budget *TokenBudget
}

func NewDriver(client llm.Client, budget *TokenBudget) *Driver {
	return &Driver{client: client, budget: budget}
}

func (d *Driver) Decide(ctx context.Context, prompt string, schema any) error {
	if d.budget.Exhausted() {
		return fmt.Errorf("token budget exhausted")
	}

	if !d.budget.CanSpend(d.budget.PerCallLimit) {
		return fmt.Errorf("insufficient budget: remaining %d, need up to %d",
			d.budget.Remaining(), d.budget.PerCallLimit)
	}

	resp, err := d.client.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return fmt.Errorf("llm call: %w", err)
	}

	d.budget.Record(resp.Usage.TotalTokens)

	if err := ParseStructuredOutput(resp.Content, schema); err != nil {
		return fmt.Errorf("parse output: %w\nraw: %s", err, resp.Content)
	}

	return nil
}

func (d *Driver) DecideWithVision(ctx context.Context, prompt string, images [][]byte, schema any) error {
	if d.budget.Exhausted() {
		return fmt.Errorf("token budget exhausted")
	}

	resp, err := d.client.CompleteWithVision(ctx, prompt, images)
	if err != nil {
		return fmt.Errorf("llm vision call: %w", err)
	}

	d.budget.Record(resp.Usage.TotalTokens)

	if err := ParseStructuredOutput(resp.Content, schema); err != nil {
		return fmt.Errorf("parse output: %w\nraw: %s", err, resp.Content)
	}

	return nil
}

func (d *Driver) Budget() *TokenBudget {
	return d.budget
}
```

- [ ] **Step 8: Run tests**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/ai/...`
Expected: PASS (7 tests).

- [ ] **Step 9: Commit**

```bash
git add ./internal/ai/
git commit -m "feat(cerberus): AI driver — budget, prompt, context, parser, Decide"
```

---

## Task 10: Session Lifecycle

**Files:**
- Create: `./internal/session/lifecycle.go`

- [ ] **Step 1: Implement lifecycle.go**

```go
// ./internal/session/lifecycle.go
package session

import (
	"context"
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Mode string

const (
	ModeRun    Mode = "run"
	ModeVerify Mode = "verify"
	ModeServe  Mode = "serve"
)

type Session struct {
	ID        uuid.UUID
	Mode      Mode
	Goal      string
	Config    *project.Config
	Store     *store.Store
	Driver    *ai.Driver
	Logger    *zap.Logger
	StartedAt time.Time
}

// NewSession creates and persists a new test session.
func NewSession(ctx context.Context, mode Mode, goal string, cfg *project.Config,
	s *store.Store, client llm.Client, logger *zap.Logger) (*Session, error) {

	budget := ai.NewTokenBudget(
		cfg.Settings.AIBudget.SessionTotalTokens,
		cfg.Settings.AIBudget.PerCallLimit,
	)

	sess := &Session{
		Mode:      mode,
		Goal:      goal,
		Config:    cfg,
		Store:     s,
		Driver:    ai.NewDriver(client, budget),
		Logger:    logger,
		StartedAt: time.Now(),
	}

	// Persist to DB
	dbSess, err := s.CreateSession(ctx, string(mode), goal, cfg.Project.Name)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	sess.ID = dbSess.ID

	logger.Info("session created",
		zap.String("id", sess.ID.String()),
		zap.String("mode", string(mode)),
		zap.String("goal", goal))

	return sess, nil
}

// Run executes the session lifecycle.
// In C1 this is a skeleton — real logic comes in C2a/C3.
func (s *Session) Run(ctx context.Context) error {
	s.Logger.Info("session starting", zap.String("id", s.ID.String()))

	// Update status to completed when done
	defer func() {
		if err := s.Store.UpdateSessionStatus(ctx, s.ID, "completed"); err != nil {
			s.Logger.Error("update session status", zap.Error(err))
		}
	}()

	// Skeleton: the real phases (cognition, planning, execution, judgment, learning)
	// will be implemented in C2a (Explorer) and C3 (Judge/Checker).
	s.Logger.Info("session completed (skeleton — no heads wired yet)",
		zap.String("id", s.ID.String()))

	return nil
}

// Close cleans up session resources.
func (s *Session) Close() {
	s.Logger.Info("session closed",
		zap.String("id", s.ID.String()),
		zap.Int("tokens_spent", s.Driver.Budget().SessionTotal-s.Driver.Budget().Remaining()))
}
```

- [ ] **Step 2: Verify build**

Run: `cd projects/cerberus && go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add ./internal/session/
git commit -m "feat(cerberus): session lifecycle (C1 skeleton)"
```

---

## Task 11: CLI Commands — init / run / verify / serve

**Files:**
- Modify: `./cmd/cerberus/main.go`

- [ ] **Step 1: Implement full main.go with cobra**

```go
// ./cmd/cerberus/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	urlFlag    string
	goalFlag   string
	actorFlags []string
	dbFlag     string
	configFlag string
	portFlag   string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cerberus",
		Short: "Cerberus — Universal AI Testing Framework",
	}

	rootCmd.AddCommand(initCmd(), runCmd(), verifyCmd(), serveCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize project configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ".cerberus"
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("create .cerberus dir: %w", err)
			}

			// project.yaml template
			projectYAML := `project:
  name: ""

services:
  - name: web
    url: "http://localhost:3000"
    health: "/"

actors:
  - name: admin
    credentials:
      email: "${ADMIN_EMAIL}"
      password: "${ADMIN_PASS}"
    entry: "/admin"

databases: []

invariants: []

settings:
  max_duration: 30m
  confidence_threshold: 0.7
  auto_fix: low_only
  ai_budget:
    session_total_tokens: 200000
    per_call_limit: 10000
    model: "claude-sonnet-4-6"
`
			if err := os.WriteFile(dir+"/project.yaml", []byte(projectYAML), 0644); err != nil {
				return err
			}

			// credentials.yaml template
			credYAML := `# Credentials — DO NOT commit this file
# Add to .gitignore
actors:
  admin:
    email: admin@example.com
    password: changeme
`
			if err := os.WriteFile(dir+"/credentials.yaml", []byte(credYAML), 0644); err != nil {
				return err
			}

			// Append credentials.yaml to .gitignore (preserve existing content)
			existing, _ := os.ReadFile(".gitignore")
			entry := ".cerberus/credentials.yaml\n"
			if !strings.Contains(string(existing), entry) {
				f, err := os.OpenFile(".gitignore", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err == nil {
					f.WriteString(entry)
					f.Close()
				}
			}

			fmt.Println("✓ Created .cerberus/project.yaml")
			fmt.Println("✓ Created .cerberus/credentials.yaml")
			fmt.Println("✓ Updated .gitignore")
			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  1. Edit .cerberus/project.yaml with your project details")
			fmt.Println("  2. Set credentials in .cerberus/credentials.yaml or env vars")
			fmt.Println("  3. Run: cerberus run --url http://localhost:3000 --goal \"test all APIs\"")
			return nil
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "Target URL")
	return cmd
}

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run intelligent tests (cognition + exploration + judgment)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer logger.Sync()

			// Load project config
			projCfg := loadProjectConfig(configFlag, urlFlag, goalFlag, logger)

			// Resolve credentials
			projCfg = project.ResolveCredentials(projCfg)

			// Setup store (SQLite)
			dbPath := cfg.DBPath
			if dbFlag != "" {
				dbPath = dbFlag
			}

			s, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer s.Close()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			// Create LLM client
			model := projCfg.Settings.AIBudget.Model
			if model == "" {
				model = cfg.LLMModel
			}
			apiKey := cfg.LLMAPIKey

			client, err := llm.NewClient(model, apiKey)
			if err != nil {
				return fmt.Errorf("create LLM client: %w", err)
			}

			// Create and run session
			sess, err := session.NewSession(ctx, session.ModeRun, goalFlag, projCfg, s, client, logger)
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}

			// Handle graceful shutdown
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			go func() {
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
				<-sigCh
				logger.Info("interrupt received, shutting down...")
				cancel()
			}()

			if err := sess.Run(ctx); err != nil {
				return fmt.Errorf("session run: %w", err)
			}

			sess.Close()
			return nil
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "Target URL (required)")
	cmd.Flags().StringVar(&goalFlag, "goal", "", "Test goal description (required)")
	cmd.Flags().StringSliceVar(&actorFlags, "actor", nil, "Actor names to use")
	cmd.Flags().StringVar(&dbFlag, "db", "", "Database URL (enables Checker)")
	cmd.Flags().StringVar(&configFlag, "config", ".cerberus/project.yaml", "Project config file")
	_ = cmd.MarkFlagRequired("url")
	_ = cmd.MarkFlagRequired("goal")
	return cmd
}

func verifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify against known project model (regression mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("verify mode: not yet implemented (requires C2a + C3)")
			return nil
		},
	}
	cmd.Flags().StringVar(&configFlag, "config", ".cerberus/project.yaml", "Project config file")
	return cmd
}

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP API server (CI integration)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("serve mode: not yet implemented (requires C5 + C6)")
			return nil
		},
	}
	cmd.Flags().StringVar(&portFlag, "port", "8090", "HTTP server port")
	return cmd
}

func loadProjectConfig(configPath, url, goal string, logger *zap.Logger) *project.Config {
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			cfg, err := project.LoadFromFile(configPath)
			if err != nil {
				logger.Warn("failed to load project config, using defaults", zap.Error(err))
				d := project.DefaultConfig()
				return &d
			}
			return cfg
		}
	}
	d := project.DefaultConfig()
	return &d
}
```

- [ ] **Step 2: Build and verify**

Run: `cd projects/cerberus && go build -o bin/cerberus ./cmd/cerberus`

Expected: builds successfully.

- [ ] **Step 3: Test init command**

Run: `cd /tmp && rm -rf test-cerberus-init && mkdir test-cerberus-init && cd test-cerberus-init && /home/mason/Documents/code_projects/private/cerberus/bin/cerberus init`

Expected: prints "✓ Created .cerberus/project.yaml" etc.

Verify: `cat /tmp/test-cerberus-init/.cerberus/project.yaml`

- [ ] **Step 4: Test help output**

Run: `./bin/cerberus --help && ./bin/cerberus run --help`

Expected: shows usage for root and run commands.

- [ ] **Step 5: Commit**

```bash
git add ./cmd/
git commit -m "feat(cerberus): CLI commands — init, run, verify (stub), serve (stub)"
```

---

## Task 12: Smoke Test

**Files:**
- Create: `./internal/smoke/smoke_test.go`

- [ ] **Step 1: Write smoke test**

```go
// ./internal/smoke/smoke_test.go
package smoke

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSessionSmokeTest(t *testing.T) {
	// Setup — in-memory SQLite, no external DB needed
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	// Use mock LLM — no API key needed
	mockResp := `{"status":"pass","confidence":0.9,"reasoning":"mock analysis"}`
	client := llm.NewMockClient(map[string]string{"default": mockResp})

	logger, _ := zap.NewDevelopment()

	// Create session
	cfg := project.DefaultConfig()
	cfg.Project.Name = "smoke-test"

	sess, err := session.NewSession(ctx, session.ModeRun, "smoke test goal", cfg, s, client, logger)
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID)

	// Run (skeleton in C1 — just creates/completes session)
	err = sess.Run(ctx)
	require.NoError(t, err)

	// Verify session in DB
	dbSess, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", dbSess.Status)
	assert.Equal(t, "smoke test goal", dbSess.Goal)

	sess.Close()
}

func TestAIDriverSmokeTest(t *testing.T) {
	mockResp := `{"verdict":"pass","confidence":0.95,"reasoning":"response matches expected"}`
	client := llm.NewMockClient(map[string]string{"default": mockResp})

	driver := ai.NewDriver(client, ai.NewTokenBudget(200000, 10000))

	type Verdict struct {
		Verdict    string  `json:"verdict"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	var v Verdict
	err := driver.Decide(context.Background(),
		ai.NewPrompt().
			System("You are a test judge").
			Task("Evaluate: POST /api/v1/users returned 201").
			Output("JSON with verdict, confidence, reasoning").
			Build(),
		&v,
	)
	require.NoError(t, err)
	assert.Equal(t, "pass", v.Verdict)
	assert.InDelta(t, 0.95, v.Confidence, 0.01)
	assert.Less(t, driver.Budget().Remaining(), 200000)
}

func TestProjectLoaderSmokeTest(t *testing.T) {
	os.Setenv("TEST_URL", "http://localhost:8080")
	defer os.Unsetenv("TEST_URL")

	yaml := `
project:
  name: smoke-app
services:
  - name: api
    url: "${TEST_URL}"
settings:
  confidence_threshold: 0.8
`
	cfg, err := project.LoadFromYAML([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "smoke-app", cfg.Project.Name)
	assert.Equal(t, "http://localhost:8080", cfg.Services[0].URL)
	assert.Equal(t, 0.8, cfg.Settings.ConfidenceThreshold)

	// Defaults should be applied
	assert.Equal(t, 200000, cfg.Settings.AIBudget.SessionTotalTokens)
}
```

Note: fix the `require.NoError(t, t, err)` typo — should be `require.NoError(t, err)`.

- [ ] **Step 2: Run unit smoke tests (no DB)**

Run: `cd projects/cerberus && go test -v -count=1 ./internal/smoke/... -run TestAIDriver`
Expected: PASS.

- [ ] **Step 3: Run full smoke test**

```bash
go test -v -count=1 ./internal/smoke/...
```

Expected: all 3 tests PASS (in-memory SQLite, no external DB needed).

- [ ] **Step 4: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: all tests PASS.

- [ ] **Step 5: Final build check**

Run: `cd projects/cerberus && make check`
Expected: fmt + lint + test all pass.

- [ ] **Step 6: Commit**

```bash
git add ./internal/smoke/
git commit -m "test(cerberus): smoke tests — session, AI driver, project loader"
```

---

## Self-Review Checklist

**1. Spec coverage check:**

| Spec Section | C1 Task |
|---|---|
| §0 Project positioning | Task 1 (module, README) |
| §1 Three-head architecture interfaces | Task 9 (types), Task 10 (lifecycle stubs) |
| §2 CLI commands | Task 11 (init/run/verify/serve) |
| §3 Run command flow | Task 10 (lifecycle skeleton), Task 11 (CLI) |
| §4 Explorer CodeProvider/KnowledgeSource | Not in C1 (C2a) |
| §5 Judge types | Not in C1 (C3) |
| §6 Checker types | Not in C1 (C3) |
| §7 Memory system | Task 3 (DB tables), Task 8 (episodic store) |
| §8 Project Plugin | Tasks 5-6 |
| §9 LLM Client | Tasks 7-8 |
| §10 AI Driver | Task 9 |
| §11 (pkg structure) | All tasks |
| §12 DB Schema | Task 3 |
| §14 Cross-cutting (budget, degradation) | Task 9 (budget), Task 11 (graceful shutdown) |

**2. Placeholder scan:**
- No TBD/TODO found
- verify/serve commands have explicit "not yet implemented" messages (not placeholders — they're intentional C1 stubs)
- Explorer/Judge/Checker are noted as C2a/C3 scope (correct)

**3. Type consistency check:**
- `project.Config` used consistently across session, loader, CLI
- `llm.Client` interface implemented by all 4 providers (Claude, OpenAI, Gemini, Mock)
- `ai.Driver` interface methods match what session.go calls
- `store.Session.ID` is `uuid.UUID` consistently
- `session.Mode` is a string type, stored as string in DB

**4. Issues fixed during plan review:**
- ✅ `require.NoError(t, t, err)` typo in smoke test — fixed to `require.NoError(t, err)`
- ✅ `session.go` `UpdateSessionStatus` dead code line — removed
- ✅ `trace.go`/`verdict.go`/`memory.go` `sessionID` parameter — changed from `interface{}` to `uuid.UUID`
- ✅ `"os"` import needed in `schema_test.go` for Task 6
- ✅ Maturity Score test assertion: `0.22` → `0.17` (math was correct in comment, assertion was wrong)
- ✅ `Session.Stats` type: `any` → `[]byte` (JSONB scan compatibility with lib/pq)
- ✅ Vision image encoding: `fmt.Sprintf("%x", img)` → `base64.StdEncoding.EncodeToString(img)` (all 3 providers + 4 call sites)
- ✅ Added `encoding/base64` import to claude.go, openai.go, gemini.go
- ✅ Removed custom `max()` function (built-in since Go 1.21+)
- ✅ `loadProjectConfig` return type: `DefaultConfig()` value → `&d` pointer
- ✅ `init` command `.gitignore`: overwrite → append (preserve existing content)
- ✅ Added `"strings"` import to main.go (needed for gitignore check)
- ✅ Gemini `baseURL()`: API key moved from URL query param to `x-goog-api-key` header
- ✅ OpenAI vision: `content2` variable renamed to `content`
- ✅ Module path: `github.com/binoctal/modelsite/projects/cerberus` → `github.com/binoctal/cerberus`
- ✅ All file paths: `projects/cerberus/` → `./` (project root)
- ✅ Spec path: `docs/superpowers/specs/...` → `docs/2026-06-06-cerberus-design.md`
- ✅ Storage: PostgreSQL → SQLite for MVP (C1-C3), PostgreSQL migration planned for C4
- ✅ Config: Host/Port/User/Password/Name → single `DBPath` field (SQLite file path)
- ✅ SQL: PostgreSQL types (UUID/JSONB/BIGSERIAL/TIMESTAMPTZ) → SQLite types (TEXT/INTEGER/REAL)
- ✅ Tests: in-memory SQLite — no external DB required for any test
- ✅ Driver: `lib/pq` → `modernc.org/sqlite` (pure Go, no CGo)
- ✅ jsonb() → jsonText() (JSON stored as TEXT in SQLite)
- ✅ Duration INTERVAL → duration_ms INTEGER (SQLite has no INTERVAL type)
- ✅ TEXT[] tags → TEXT '[]' (SQLite has no array type, use JSON string)

---

## Follow-up Plans

| Plan | Phase | Scope |
|------|-------|-------|
| C2a | Explorer API exploration | Explorer head, CodeProvider (patternscan/manifest/openapi), Recon, Planner, Executor (API only) |
| C2b | Browser exploration | Playwright MCP integration, browser actions, snapshot |
| C3 | Judge + Checker + Arbitrator | Judge head, Checker head, Arbitrator, Evidence Bus |
| C4 | Memory system | L2 semantic memory, L3 procedural, embedding (Ollama/OpenAI/tsvector), search.go, learner.go |
| C5 | Session + Store + Reports | Port report/fixer modules, session history, coverage tracking |
| C6 | Server + Integration | serve mode HTTP API, integration tests, documentation |
