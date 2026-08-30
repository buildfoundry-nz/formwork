//go:build ignore

package extraction

// listSkus resolves a sku's identity from the entity.
func listSkus(ctx context.Context, tx pgx.Tx, projectID string) []Sku {
	return projectparses.EntitySkus(ctx, tx, projectID)
}
