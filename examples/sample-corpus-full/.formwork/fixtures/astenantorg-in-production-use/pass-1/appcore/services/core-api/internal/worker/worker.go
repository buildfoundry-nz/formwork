//go:build ignore

package worker

// The worker seam is adopted: a production call site exists.
func run(ctx context.Context, tenantID string) error {
	return db.AsTenantOrg(ctx, func() error { return doWork(tenantID) })
}
