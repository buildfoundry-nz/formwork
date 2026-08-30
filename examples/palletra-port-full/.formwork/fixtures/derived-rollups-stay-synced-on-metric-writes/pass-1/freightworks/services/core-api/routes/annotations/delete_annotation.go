//go:build ignore

package annotations

import "context"

// DeleteAnnotation removes one annotation and re-syncs the cascade-only derived
// rollups in the same transaction.
func DeleteAnnotation(ctx context.Context, tx Tx, sheetID, code, id string) error {
	if _, err := tx.Exec(ctx, "DELETE FROM palletra.annotations WHERE id = $1", id); err != nil {
		return err
	}
	return shared.SyncDerivedGaugeApproval(ctx, tx, sheetID, code)
}
