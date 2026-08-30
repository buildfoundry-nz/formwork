//go:build ignore

package extraction

// addToBOM dropped the detect half of the shared guard — the template gate is
// blind to an authored line already representing this sku.
func addToBOM(ctx context.Context, tx pgx.Tx, in AppendInput) error {
	resolution := ResolveSkuLineConflict(ctx, tx, in.RatedItemID)
	return insertPresetLine(ctx, tx, in, resolution)
}
