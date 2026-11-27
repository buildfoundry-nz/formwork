//go:build ignore

package project

// FIRE: ResolveTier has no extraction floor-level fallback.
func ResolveTier(p Page) string {
	return p.ManualTier
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// return p.tierLevel
