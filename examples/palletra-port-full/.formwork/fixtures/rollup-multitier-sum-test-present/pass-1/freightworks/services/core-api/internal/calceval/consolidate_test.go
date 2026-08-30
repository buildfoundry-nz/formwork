//go:build ignore

package calceval

func TestDedupCrossPageRollupRows_MultiTier_NullUnit_SumsAcrossFloorLevels(t *testing.T) {
	assertSum(t, dedup(rows), 8)
}
