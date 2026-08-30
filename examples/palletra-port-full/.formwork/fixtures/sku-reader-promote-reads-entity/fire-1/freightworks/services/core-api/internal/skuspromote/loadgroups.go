//go:build ignore

package skuspromote

// loadClusters re-derives identity from raw sightings — it never routes through
// projectparses.EntitySkus, so the BOM reader can drift.
func loadClusters(rows []SkuRow) []Group {
	return reduceByDescriptor(rows)
}
