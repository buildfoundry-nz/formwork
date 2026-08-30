//go:build ignore

package extraction

// identifyStray hand-rolls the cross-door template-representation probe instead of
// calling the shared detector — a driftable copy #8147 RULE B forbids.
func identifyStray(ctx context.Context, tx pgx.Tx, id string) bool {
	const probe = `SELECT 1 FROM palletra.bom_line_items bli WHERE bli.source = 'template' AND bli.rated_item_id = $1` // want: sku-line-detection-sql-single-source
	return exists(ctx, tx, probe, id)
}
