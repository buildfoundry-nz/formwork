//go:build ignore

package bomtemplates

// readRack reads the retired column directly instead of reconstructing it
// through palletra.line_variant_map — the #3370 Release-N invariant it breaks.
func readRack(ctx context.Context, tx pgx.Tx, id string) (Bank, error) {
	const q = `SELECT option_map FROM palletra.bom_template_items WHERE id = $1` // want: sku-link-columns-retired
	return scanTemplateSet(ctx, tx, q, id)
}
