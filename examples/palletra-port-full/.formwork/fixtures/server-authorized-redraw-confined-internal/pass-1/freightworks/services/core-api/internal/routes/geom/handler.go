//go:build ignore

package geom

// A route reaching the geometry seam does so as a ManualEdit — the K13 lock
// applies and the bypass token is never named here.
func handleWaiver(ctx context.Context, g Geometry) error {
	return mutate(ctx, g, mutbase.ManualEdit)
}
