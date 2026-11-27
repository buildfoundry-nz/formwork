//go:build ignore

package extraction

// addToBOM dropped the resolve half of the shared guard — it detects the
// conflict but never enforces the estimator's chosen outcome.
func addToBOM(ctx context.Context, tx pgx.Tx, in AppendInput) error {
	conflict := DetectSkuLineConflict(ctx, tx, in.RatedItemID)
	return insertPresetLine(ctx, tx, in, conflict)
}
