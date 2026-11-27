//go:build ignore

package upload

func finalize(ctx context.Context, tx pgx.Tx) error {
	mirror, err := phasefeed.LogInTx(ctx, tx, transition, claim)
	if err != nil {
		return err
	}
	return mirror.FlushAfterCommit()
}
