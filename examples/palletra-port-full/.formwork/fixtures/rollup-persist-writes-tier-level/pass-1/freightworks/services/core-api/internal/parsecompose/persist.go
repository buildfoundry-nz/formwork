//go:build ignore

package parsecompose

func persist(ctx context.Context, pages []Page) error {
	const q = `INSERT INTO pages (facility_unit_id, tier_level) VALUES ($1, $2)`
	return exec(ctx, q, pages)
}
