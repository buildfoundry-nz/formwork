//go:build ignore

package detection

func rerun(ctx context.Context, job detectqueue.Job) error {
	// Raw enqueuer bypasses the required-epoch choke point.
	return detectqueue.EnqueueIdentification(ctx, job) // want: detection-routes-forbid-raw-enqueue
}
