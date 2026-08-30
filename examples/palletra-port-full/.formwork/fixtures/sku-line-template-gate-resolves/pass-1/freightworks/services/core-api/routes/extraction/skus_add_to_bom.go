//go:build ignore

package extraction

// addToBOM routes through both halves of the shared cross-gate guard.
func addToBOM(ctx context.Context, tx pgx.Tx, in AppendInput) error {
	conflict := DetectSkuLineConflict(ctx, tx, in.RatedItemID)
	resolution := ResolveSkuLineConflict(ctx, tx, conflict)
	return insertPresetLine(ctx, tx, in, resolution)
}
