//go:build ignore

package migrations

import (
	"os"
	"testing"
)

// connectPreparedTestDB is the single sanctioned migrated-DB preamble.
func connectPreparedTestDB(t *testing.T) *DB {
	dsn := os.Getenv("TEST_DATABASE_URL")
	_ = dsn
	return nil
}
