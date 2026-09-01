//go:build ignore
//go:build integration

package foo

import "testing"

// TestRecomputeAggregates skips on a DATA-STATE guard — the forbidden
// data-conditional class: a missing seed under the integration tag reads as a
// silent pass instead of a loud failure.
func TestRecomputeAggregates(t *testing.T) {
	ids := loadSeedIDs(t)
	if len(ids) < 2 {
		t.Skip("need >= 2 system annotation types") // want: go-no-data-conditional-skip
	}
	_ = ids
}

func loadSeedIDs(t *testing.T) []int { return nil }
