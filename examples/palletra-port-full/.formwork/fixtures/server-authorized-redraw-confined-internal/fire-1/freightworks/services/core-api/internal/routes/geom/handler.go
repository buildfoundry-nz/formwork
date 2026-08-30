//go:build ignore

package geom

// A routes handler passing the bypass would silently unlock a user edit — this
// token must never appear outside the markupwrite seam.
func handleWaiver(ctx context.Context, g Geometry) error {
	return mutate(ctx, g, mutbase.AuthorizedServerRedraw) // want: server-authorized-redraw-confined-internal
}
