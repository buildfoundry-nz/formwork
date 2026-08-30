//go:build ignore

package skuspromote

// loadClusters resolves a sku's identity from the entity.
func loadClusters(ctx context.Context, tx pgx.Tx, projectID string) []Sku {
	return projectparses.EntitySkus(ctx, tx, projectID)
}
