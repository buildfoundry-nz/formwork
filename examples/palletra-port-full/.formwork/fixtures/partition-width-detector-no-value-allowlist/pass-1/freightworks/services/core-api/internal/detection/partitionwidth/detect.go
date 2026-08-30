//go:build ignore

package partitionwidth

// The single range cap; any integer callout in [1, MaxPartitionWidthMm] is a candidate.
const MaxPartitionWidthMm = 500

func detect(callouts []int) int {
	return highestInRange(callouts, MaxPartitionWidthMm)
}
