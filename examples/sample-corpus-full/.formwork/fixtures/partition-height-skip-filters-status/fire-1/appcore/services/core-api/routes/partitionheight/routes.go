//go:build ignore

package partitionheight

func Register(r Router) {
	RegisterPartitionHeightPropagate(r) // want: partition-height-skip-filters-status
}
