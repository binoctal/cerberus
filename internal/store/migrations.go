package store

import (
	"context"
	"database/sql"
)

type migration struct {
	version  int
	filename string
}

func RunMigrations(ctx context.Context, db *sql.DB, dir string) error {
	// Phase 1: Create migrations table
	if err := createMigrationsTable(ctx, db); err != nil {
		return err
	}

	// Phase 2: Discover migration files
	migrations, err := discoverMigrations(dir)
	if err != nil {
		return err
	}

	// Phase 3: Apply each migration
	for _, m := range migrations {
		if err := applySingleMigration(ctx, db, dir, m); err != nil {
			return err
		}
	}

	return nil
}
