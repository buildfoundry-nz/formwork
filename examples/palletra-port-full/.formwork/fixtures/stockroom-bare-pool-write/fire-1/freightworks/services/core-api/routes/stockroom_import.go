//go:build ignore

package routes

import "context"

// A bare-pool write from a stockroom handler — runs with no tenant GUC, bypassing
// RLS. Forbidden.
func (h *Handler) backfillVectors(ctx context.Context, id string) error {
	_, err := h.db.Exec(ctx, `UPDATE platform.sku_catalog SET embedding = NULL WHERE id = $1`, id) // want: stockroom-bare-pool-write
	return err
}
