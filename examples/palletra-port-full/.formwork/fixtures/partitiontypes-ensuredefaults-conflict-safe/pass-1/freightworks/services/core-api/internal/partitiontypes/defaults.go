//go:build ignore

package partitiontypes

import "context"

// GuaranteeDefaults seeds one confirmed PRIMARY partition type per side on the List path.
// The NOT-EXISTS-guarded INSERT carries ON CONFLICT DO NOTHING, so a concurrent
// double-seed is absorbed as a no-op instead of 500-ing on a 23505.
func GuaranteeDefaults(ctx context.Context, tx pgx.Tx, projectID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO palletra.project_partition_types (project_id, side, label, is_primary)
		SELECT $1, s.side, 'Default', true
		  FROM (VALUES ('internal'), ('external')) AS s(side)
		 WHERE NOT EXISTS (
		     SELECT 1 FROM palletra.project_partition_types w
		      WHERE w.project_id = $1 AND w.side = s.side
		 )
		ON CONFLICT DO NOTHING`, projectID)
	return err
}
