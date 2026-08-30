//go:build ignore

package parsecompose

// FIRE: the extracted page rows never write the scope-key column.
func persist(ctx context.Context, pages []Page) error {
	const q = `INSERT INTO pages (facility_unit_id) VALUES ($1)`
	return exec(ctx, q, pages)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// const q = `INSERT INTO pages (facility_unit_id, tier_level) VALUES ($1, $2)`
