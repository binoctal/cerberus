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

// createMigrationsTable creates the schema_migrations table if it doesn't exist
func createMigrationsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}
	return nil
}

// discoverMigrations reads and parses migration files from directory
func discoverMigrations(dir string) ([]migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
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
		_, _ = fmt.Sscanf(matches[1], "%d", &ver)
		migrations = append(migrations, migration{version: ver, filename: e.Name()})
	}

	// Sort by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	return migrations, nil
}

// isMigrationApplied checks if a migration version has already been applied
func isMigrationApplied(ctx context.Context, db *sql.DB, version int) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check migration %d: %w", version, err)
	}
	return exists, nil
}

// applySingleMigration applies a single migration within a transaction
func applySingleMigration(ctx context.Context, db *sql.DB, dir string, m migration) error {
	// Check if already applied
	applied, err := isMigrationApplied(ctx, db, m.version)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	// Read migration file
	content, err := os.ReadFile(dir + "/" + m.filename)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", m.filename, err)
	}

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for migration %d: %w", m.version, err)
	}

	// Execute migration SQL
	if _, err := tx.Exec(string(content)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply migration %s: %w", m.filename, err)
	}

	// Record migration
	if _, err := tx.Exec(
		"INSERT INTO schema_migrations (version, name) VALUES (?, ?)",
		m.version, m.filename); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record migration %d: %w", m.version, err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", m.version, err)
	}

	return nil
}
