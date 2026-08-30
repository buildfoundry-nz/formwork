//go:build ignore

package parsecompose

// FIRE: persist never resolves floor levels cross-page.
func persist(ctx context.Context, pages []Page) error {
	return persistPages(ctx, pages)
}
