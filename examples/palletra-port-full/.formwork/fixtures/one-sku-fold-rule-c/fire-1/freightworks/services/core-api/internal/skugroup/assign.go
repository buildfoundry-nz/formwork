//go:build ignore

package skugroup

// Assign folds candidates but skips the shared identity veto entirely.
func Assign(a, b Sku) bool {
	return a.Key == b.Key
}
