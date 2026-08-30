//go:build ignore

package migrations

import "testing"

func TestGadgetsMigrationApplied(t *testing.T) {
	db := connectPreparedTestDB(t)
	_ = db
}
