//go:build ignore

package parsecompose

func persist(ctx context.Context, pages []Page) error {
	pages = resolveTierLevels(ctx, pages)
	return persistPages(ctx, pages)
}
