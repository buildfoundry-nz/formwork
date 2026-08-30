//go:build ignore

package skuspromote

// CreateOriginated routes through both halves of the shared cross-gate guard.
func CreateOriginated(ctx context.Context, tx pgx.Tx, in ManualInput) error {
	conflict := DetectSkuLineConflict(ctx, tx, in.ProjectSkuID)
	resolution := ResolveSkuLineConflict(ctx, tx, conflict)
	return insertManualLine(ctx, tx, in, resolution)
}
