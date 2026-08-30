//go:build ignore

package partitiontypes

// READ approved metrics to seed drafts; never write them back.
func loadApprovedPartitionHeights(ctx context.Context, tx pgx.Tx) ([]Row, error) {
	rows, err := query(ctx, tx, `SELECT value FROM palletra.annotation_gauges WHERE approved`)
	return rows, err
}

func saveType(ctx context.Context, tx pgx.Tx, code string) error {
	_, err := tx.Exec(ctx, `UPDATE palletra.project_partition_types SET partition_type_code = $1`, code)
	return err
}
