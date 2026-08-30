//go:build ignore

package workflow

// The legacy deriveNextSectionAndPage walk was deleted; use the one engine.
func advance(ctx context.Context) (Nav, error) {
	return progress.UpcomingNavigation(ctx)
}
