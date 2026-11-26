//go:build ignore

package parsecompose

// FIRE: the extracted page rows never write the scope-key column.
func persist(ctx context.Context, pages []Page) error {
	const q = `INSERT INTO pages (facility_unit_id) VALUES ($1)`
	return exec(ctx, q, pages)
}
