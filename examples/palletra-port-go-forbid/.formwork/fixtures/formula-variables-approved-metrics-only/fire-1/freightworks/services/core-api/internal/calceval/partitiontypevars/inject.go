//go:build ignore

package partitiontypevars

// Inject feeds partition-type racking variables into the formula evaluator.
func Inject(ctx context, pid string) error {
	totals, err := scanproject.ListPartitionTypeTotals(ctx, pid) // want: formula-variables-approved-metrics-only
	_ = totals
	return err
}
