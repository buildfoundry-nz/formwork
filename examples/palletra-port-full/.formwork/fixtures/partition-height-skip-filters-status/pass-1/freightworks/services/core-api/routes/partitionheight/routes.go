//go:build ignore

package partitionheight

func Register(r Router) {
	// manual partition-height create is the supported flow
	registerManualPartitionHeightCreate(r)
}
