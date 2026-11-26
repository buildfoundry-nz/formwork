//go:build ignore

package parsecompose

// FIRE: persist never resolves floor levels cross-page.
func persist(ctx context.Context, pages []Page) error {
	return persistPages(ctx, pages)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// pages = resolveTierLevels(ctx, pages)
