//go:build ignore

package worker

// This worker still runs as AsSuperuser; the tenant seam is defined but not adopted.
func run(ctx context.Context) error {
	return db.AsSuperuser(ctx, doWork)
}
