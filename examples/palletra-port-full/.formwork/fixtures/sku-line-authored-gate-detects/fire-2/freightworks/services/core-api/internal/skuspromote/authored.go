//go:build ignore

package skuspromote

// CreateOriginated dropped the detect half of the shared guard — it resolves a
// conflict it never detected, so a double-line slips through.
func CreateOriginated(ctx context.Context, tx pgx.Tx, in ManualInput) error {
	resolution := ResolveSkuLineConflict(ctx, tx, in.ProjectSkuID)
	return insertManualLine(ctx, tx, in, resolution)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// conflict := DetectSkuLineConflict(ctx, tx, in.ProjectSkuID)
