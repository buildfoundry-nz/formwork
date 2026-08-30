//go:build ignore
//go:build integration

package foo

import (
	"os"
	"testing"
)

// TestRecomputeTotals skips only on a permitted opt-in env probe
// (os.Getenv) — an env-conditional guard, not a data-conditional one.
func TestRecomputeTotals(t *testing.T) {
	if os.Getenv("RUN_PERF_GATES") == "" {
		t.Skip("RUN_PERF_GATES not set")
	}
	ids := loadFixtureIDs(t)
	if len(ids) < 2 {
		t.Fatal("integration seed missing: expected >= 2 system annotation types")
	}
	_ = ids
}

func loadFixtureIDs(t *testing.T) []int { return nil }
