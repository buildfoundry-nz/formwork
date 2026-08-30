//go:build ignore

package pricing

// Route through the canonical producer instead of a local helper.
func resolvedKey(raw string) string {
	return stockroom.CanonicalizeKey(raw)
}
