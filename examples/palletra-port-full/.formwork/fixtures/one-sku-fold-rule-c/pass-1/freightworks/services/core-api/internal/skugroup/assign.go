//go:build ignore

package skugroup

import "example.com/api/internal/skuidentity"

// Assign folds candidates but consults the shared identity veto first.
func Assign(a, b Sku) bool {
	if skuidentity.Conflict(a, b) {
		return false
	}
	return true
}
