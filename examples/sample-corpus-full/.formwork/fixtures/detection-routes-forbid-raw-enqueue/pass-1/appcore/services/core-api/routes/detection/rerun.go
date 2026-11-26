//go:build ignore

package detection

func rerun(ctx context.Context, tx pgx.Tx, job detectqueue.Job, sheetIDs []string) error {
	epochs, err := detectionepoch.Allocate(ctx, tx, sheetIDs)
	if err != nil {
		return err
	}
	return detectqueue.EnqueueIdentificationAtEpoch(ctx, job, epochs)
}
