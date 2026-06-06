package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunMigrations(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer s.Close()

	ctx := context.Background()

	// Verify tables exist (SQLite uses sqlite_master)
	var count int
	err := s.DB().QueryRowContext(ctx,
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
