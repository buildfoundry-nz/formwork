//go:build ignore

package bomtemplates

// The retired option_map and default_priced_line_id columns are reconstructed
// through the sanctioned SQL functions (this comment is stripped before match).
func readRack(ctx context.Context, tx pgx.Tx, id string) (Bank, error) {
	const q = `SELECT sample.line_variant_map(id) AS bank, sample.item_default_rate_line(id) AS dflt FROM sample.bom_template_items WHERE id = $1`
	return scanTemplateSet(ctx, tx, q, id)
}
