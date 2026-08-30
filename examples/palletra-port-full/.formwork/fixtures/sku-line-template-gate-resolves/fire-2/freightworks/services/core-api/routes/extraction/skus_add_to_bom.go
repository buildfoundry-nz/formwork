//go:build ignore

package extraction

// addToBOM dropped the resolve half of the shared guard — it detects the
// conflict but never enforces the estimator's chosen outcome.
func addToBOM(ctx context.Context, tx pgx.Tx, in AppendInput) error {
	conflict := DetectSkuLineConflict(ctx, tx, in.RatedItemID)
	return insertPresetLine(ctx, tx, in, conflict)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// resolution := ResolveSkuLineConflict(ctx, tx, conflict)
