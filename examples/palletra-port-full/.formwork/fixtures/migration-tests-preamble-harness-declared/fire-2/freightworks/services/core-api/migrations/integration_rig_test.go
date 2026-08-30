//go:build ignore

package migrations

import "testing"

// The sanctioned preamble has been deleted; the harness no longer declares it.
func TestSomething(t *testing.T) {
	t.Skip("no migrated-DB connect helper here")
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// func connectPreparedTestDB(t *testing.T) *DB {
