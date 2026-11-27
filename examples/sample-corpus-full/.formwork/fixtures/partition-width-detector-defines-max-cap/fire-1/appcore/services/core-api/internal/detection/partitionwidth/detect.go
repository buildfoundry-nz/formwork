//go:build ignore

package partitionwidth

// Regression: the single cap constant was deleted, so the arbitrary-width
// vocabulary has nothing to bound it.
func detect(callouts []int) int {
	return highest(callouts)
}
