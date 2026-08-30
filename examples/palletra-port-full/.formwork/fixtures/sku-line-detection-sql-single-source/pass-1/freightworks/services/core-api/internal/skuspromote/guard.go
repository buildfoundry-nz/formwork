//go:build ignore

package skuspromote

// DetectSkuLineConflict is the ONE sanctioned home for the cross-door
// template-representation probe (guard.go is the single detector).
func DetectSkuLineConflict(ctx context.Context, tx pgx.Tx, id string) bool {
	const probe = `SELECT 1 FROM palletra.bom_line_items bli WHERE bli.source = 'template' AND bli.rated_item_id = $1`
	return exists(ctx, tx, probe, id)
}
