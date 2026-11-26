//go:build ignore

package pricing

// A non-canonical helper that produces a normalized key outside stockroom.
func canonicalizeItemKey(raw string) string { // want: stockroom-normalize-canonical
	return toLower(raw)
}
