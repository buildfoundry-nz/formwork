//go:build ignore

package partitionwidth

// Regression: the single cap constant was deleted, so the arbitrary-width
// vocabulary has nothing to bound it.
func detect(callouts []int) int {
	return highest(callouts)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// const MaxPartitionWidthMm = 500
