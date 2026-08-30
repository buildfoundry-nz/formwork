//go:build ignore

package pricing

// Regression: the upload route no longer calls ScanAndEncode, stranding the
// wizard with a nil payload.
func handleImport(ctx context.Context) error {
	return persist(ctx)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// payload, err := stockroomanalyze.ScanAndEncode(ctx, file)
