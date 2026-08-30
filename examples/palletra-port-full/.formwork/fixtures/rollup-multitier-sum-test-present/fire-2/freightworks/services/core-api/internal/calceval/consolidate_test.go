//go:build ignore

package calceval

// FIRE: the production-shape multi-tier SUM regression test is gone.
func TestDedup_Basic(t *testing.T) {
	assertSum(t, dedup(rows), 4)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// func TestDedupCrossPageRollupRows_MultiTier_NullUnit_SumsAcrossFloorLevels(t *testing.T) {
