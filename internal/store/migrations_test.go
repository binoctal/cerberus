package store

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunMigrations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s, cleanup := testStore(t)
	defer cleanup()

	ctx := context.Background()
	err := RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	var count int
	err = s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'").Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 7, "expected at least 7 tables")

	err = RunMigrations(ctx, s.DB(), "../../migrations")
	assert.NoError(t, err)
}

func testStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dbURL := os.Getenv("CERBERUS_TEST_DB_URL")
	if dbURL == "" {
		dbURL = "postgres://cerberus:cerberus@localhost:5432/cerberus_test?sslmode=disable"
	}
	s, err := New(dbURL)
	require.NoError(t, err, "connect to test DB — create cerberus_test DB first")
	return s, func() { s.Close() }
}
