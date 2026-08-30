//go:build ignore

package skuspromote

// CreateOriginated dropped the resolve half of the shared guard — it detects the
// conflict but never enforces the estimator's chosen outcome.
func CreateOriginated(ctx context.Context, tx pgx.Tx, in ManualInput) error {
	conflict := DetectSkuLineConflict(ctx, tx, in.ProjectSkuID)
	return insertManualLine(ctx, tx, in, conflict)
}
