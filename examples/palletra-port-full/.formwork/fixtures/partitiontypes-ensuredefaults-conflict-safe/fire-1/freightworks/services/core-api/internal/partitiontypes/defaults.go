//go:build ignore

package partitiontypes

import "context"

// GuaranteeDefaults lazily seeds one confirmed PRIMARY partition type per side on the
// List path, guarded by NOT EXISTS — but the INSERT lacks ON CONFLICT, so two
// concurrent first-load GETs both pass the guard and the loser 500s on a 23505.
func GuaranteeDefaults(ctx context.Context, tx pgx.Tx, projectID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO palletra.project_partition_types (project_id, side, label, is_primary) -- want: partitiontypes-ensuredefaults-conflict-safe
		SELECT $1, s.side, 'Default', true
		  FROM (VALUES ('internal'), ('external')) AS s(side)
		 WHERE NOT EXISTS (
		     SELECT 1 FROM palletra.project_partition_types w
		      WHERE w.project_id = $1 AND w.side = s.side
		 )`, projectID)
	return err
}
