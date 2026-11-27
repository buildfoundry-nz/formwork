//go:build ignore

package skuspromote

// loadClusters resolves identity from the entity — the old fold stays dead.
func loadClusters(ctx context.Context, tx pgx.Tx, projectID string) []Sku {
	return projectparses.EntitySkus(ctx, tx, projectID)
}
