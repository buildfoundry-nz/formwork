//go:build ignore

package project

// FIRE: the tier resolver has been renamed away to a weaker identifier.
func computeMetrics(pages []Page) int {
	return len(pages)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// func ResolveTier(p Page) string {
