//go:build ignore

package skuspromote

// CreateOriginated dropped the resolve half of the shared guard — it detects the
// conflict but never enforces the estimator's chosen outcome.
func CreateOriginated(ctx context.Context, tx pgx.Tx, in ManualInput) error {
	conflict := DetectSkuLineConflict(ctx, tx, in.ProjectSkuID)
	return insertManualLine(ctx, tx, in, conflict)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// resolution := ResolveSkuLineConflict(ctx, tx, conflict)
