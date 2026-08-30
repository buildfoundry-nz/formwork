//go:build ignore

package geometrycompact

import "context"

// packOne predicates the UPDATE on the captured bytes (AND layout_payload =
// $3), so a row a concurrent re-extract changed since the read is skipped, not
// clobbered — a compare-and-swap.
func packOne(ctx context.Context, tx execer, sheetID string) error {
	_, err := tx.Exec(ctx, `UPDATE palletra.page_layout SET layout_payload = $1 WHERE page_id = $2 AND layout_payload = $3`)
	return err
}
