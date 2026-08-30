//go:build ignore

package pricing

// Regression: the upload route no longer calls ScanAndEncode, stranding the
// wizard with a nil payload.
func handleImport(ctx context.Context) error {
	return persist(ctx)
}
