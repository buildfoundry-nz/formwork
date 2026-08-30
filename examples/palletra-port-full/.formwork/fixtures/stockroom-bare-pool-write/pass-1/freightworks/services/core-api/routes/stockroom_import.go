//go:build ignore

package routes

import "context"

// The write flows through db.WithOrg, which sets the per-request tenant GUC
// so RLS scopes the statement. Passing h.db as an ARGUMENT is the canonical
// seam, not a method call on the pool.
func (h *Handler) backfillVectors(ctx context.Context, claims Claims, id string) error {
	return db.WithOrg(ctx, h.db, claims, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE platform.sku_catalog SET embedding = NULL WHERE id = $1`, id)
		return err
	})
}
