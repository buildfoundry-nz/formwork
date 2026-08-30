//go:build ignore

package skugroup

// Assign folds candidates but skips the shared identity veto entirely.
func Assign(a, b Sku) bool {
	return a.Key == b.Key
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// if skuidentity.Conflict(a, b) {
