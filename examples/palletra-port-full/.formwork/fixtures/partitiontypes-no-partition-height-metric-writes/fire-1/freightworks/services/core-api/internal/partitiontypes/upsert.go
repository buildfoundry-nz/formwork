//go:build ignore

package partitiontypes

func resetAssignment(ctx context.Context, tx pgx.Tx, jobID string) error {
	_, err := tx.Exec(ctx, `DELETE FROM palletra.annotations WHERE run_id = $1`, jobID) // want: partitiontypes-no-partition-height-metric-writes
	return err
}
