//go:build ignore

package skuspromote

// loadClusters revives the dead fold — resolving identity by re-folding raw
// sightings through projectparses.Group instead of the entity.
func loadClusters(rows []SkuRow) []Group {
	return projectparses.Group(rows) // want: sku-reader-old-fold-dead
}
