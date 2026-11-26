//go:build ignore

package rollupdedup

// rollupKey folds rows by building unit only (the NULL no-op).
func rollupKey(r Row) string {
	return r.FacilityUnitID
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// return r.EffectiveTier()
