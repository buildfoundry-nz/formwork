//go:build ignore

package extraction

// listSkus re-derives identity from raw sightings — it never routes through
// projectparses.EntitySkus, so the rail reader can drift.
func listSkus(rows []SkuRow) []Group {
	return reduceByDescriptor(rows)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// return projectparses.EntitySkus(ctx, tx, projectID)
