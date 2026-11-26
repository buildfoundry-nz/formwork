//go:build ignore

package detection

func redetect(ctx context.Context, job detectqueue.Job, epochs map[string]int) error {
	// Dispatches at a hand-built epoch map with no real per-page bump.
	return detectqueue.EnqueueIdentificationAtEpoch(ctx, job, epochs) // want: detection-rerun-must-allocate-epoch
}
