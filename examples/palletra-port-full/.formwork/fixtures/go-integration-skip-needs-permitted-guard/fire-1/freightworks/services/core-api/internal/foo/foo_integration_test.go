//go:build ignore
//go:build integration

package foo

import "testing"

// TestRecomputeTotals skips on a DATA-STATE guard — the forbidden
// data-conditional class: a missing seed under the integration tag reads as a
// silent pass instead of a loud failure.
func TestRecomputeTotals(t *testing.T) {
	ids := loadFixtureIDs(t)
	if len(ids) < 2 {
		t.Skip("need >= 2 system annotation types") // want: go-integration-skip-needs-permitted-guard
	}
	_ = ids
}

func loadFixtureIDs(t *testing.T) []int { return nil }
