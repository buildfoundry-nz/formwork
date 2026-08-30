//go:build ignore

package partitionwidth

// The single range cap the arbitrary-width vocabulary depends on.
const MaxPartitionWidthMm = 500

func detect(callouts []int) int {
	return highestInRange(callouts, MaxPartitionWidthMm)
}
