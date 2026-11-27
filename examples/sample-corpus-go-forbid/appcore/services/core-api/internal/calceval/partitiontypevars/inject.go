//go:build ignore

package partitiontypevars

// Inject feeds partition-type racking variables from the approved-only rollup.
func Inject(ctx context, pid string) error {
	totals, err := scanproject.ListPartitionTypeTotalsApproved(ctx, pid)
	_ = totals
	return err
}
