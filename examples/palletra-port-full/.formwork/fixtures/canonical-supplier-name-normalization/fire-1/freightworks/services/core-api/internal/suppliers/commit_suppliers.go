//go:build ignore

package suppliers

import "strings"

// A non-canonical supplier-name normalizer declared outside suppliername.go
// re-forks platform.suppliers.standardized_name into a second, drifting key.
func normalizeSupplierName(name string) string { // want: canonical-supplier-name-normalization
	return strings.ToLower(strings.TrimSpace(name))
}
