//go:build ignore

package skuspromote

// CreateOriginated dropped the detect half of the shared guard — it resolves a
// conflict it never detected, so a double-line slips through.
func CreateOriginated(ctx context.Context, tx pgx.Tx, in ManualInput) error {
	resolution := ResolveSkuLineConflict(ctx, tx, in.ProjectSkuID)
	return insertManualLine(ctx, tx, in, resolution)
}
