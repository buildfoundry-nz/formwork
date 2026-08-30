//go:build ignore

package calceval

// FIRE: the production-shape multi-tier SUM regression test is gone.
func TestDedup_Basic(t *testing.T) {
	assertSum(t, dedup(rows), 4)
}
