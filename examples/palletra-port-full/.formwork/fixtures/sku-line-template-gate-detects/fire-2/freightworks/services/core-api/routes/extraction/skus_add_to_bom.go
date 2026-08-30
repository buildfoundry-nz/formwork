//go:build ignore

package extraction

// addToBOM dropped the detect half of the shared guard — the template gate is
// blind to an authored line already representing this sku.
func addToBOM(ctx context.Context, tx pgx.Tx, in AppendInput) error {
	resolution := ResolveSkuLineConflict(ctx, tx, in.RatedItemID)
	return insertPresetLine(ctx, tx, in, resolution)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// conflict := DetectSkuLineConflict(ctx, tx, in.RatedItemID)
