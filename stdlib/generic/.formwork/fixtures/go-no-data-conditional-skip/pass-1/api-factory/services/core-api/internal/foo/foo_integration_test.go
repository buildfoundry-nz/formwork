//go:build ignore
//go:build integration

package foo

import (
	"os"
	"testing"
)

// TestRecomputeAggregates skips only on a permitted opt-in env probe
// (os.Getenv) — an env-conditional guard, not a data-conditional one.
func TestRecomputeAggregates(t *testing.T) {
	if os.Getenv("RUN_PERF_GATES") == "" {
		t.Skip("RUN_PERF_GATES not set")
	}
	ids := loadSeedIDs(t)
	if len(ids) < 2 {
		t.Fatal("integration seed missing: expected >= 2 system annotation types")
	}
	_ = ids
}

func loadSeedIDs(t *testing.T) []int { return nil }
