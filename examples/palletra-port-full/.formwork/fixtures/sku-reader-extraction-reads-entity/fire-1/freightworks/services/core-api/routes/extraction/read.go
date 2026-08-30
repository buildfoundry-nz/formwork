//go:build ignore

package extraction

// listSkus re-derives identity from raw sightings — it never routes through
// projectparses.EntitySkus, so the rail reader can drift.
func listSkus(rows []SkuRow) []Group {
	return reduceByDescriptor(rows)
}
