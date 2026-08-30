//go:build ignore

package skus

import "context"

// A second writer of the authored pointer — must go through internal/skuspromote.
func reassign(ctx context.Context, tx DbTx, rowRef, skuID string) error {
	_, err := tx.Exec(ctx, "UPDATE palletra.bom_line_items SET project_sku_id = $1 WHERE id = $2", skuID, rowRef) // want: authored-sku-pointer-single-writer
	return err
}
