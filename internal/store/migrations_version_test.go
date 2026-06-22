package store

import (
	"os"
	"regexp"
	"testing"
)

// Two files sharing a V### prefix silently skip the second: the migration
// runner keys applied-state by version number alone.
func TestMigrationVersionNumbersAreUnique(t *testing.T) {
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Skipf("migrations dir not readable: %v", err)
	}
	re := regexp.MustCompile(`^V(\d+)_`)
	byVersion := map[string][]string{}
	for _, e := range entries {
		m := re.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		byVersion[m[1]] = append(byVersion[m[1]], e.Name())
	}
	for v, files := range byVersion {
		if len(files) > 1 {
			t.Fatalf("migration version %s claimed by multiple files: %v — the runner skips all but one",
				v, files)
		}
	}
}
