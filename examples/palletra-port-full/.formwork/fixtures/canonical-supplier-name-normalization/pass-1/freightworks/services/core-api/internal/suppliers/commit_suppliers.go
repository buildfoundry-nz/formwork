//go:build ignore

package suppliers

import "github.com/palletra/freightworks/services/core-api/internal/stockroom"

// commitSupplierRow buckets by the canonical key from the single source of truth,
// stockroom.NormalizeSupplierName — it declares no local normalizer of its own.
func commitSupplierRow(name string) string {
	return stockroom.NormalizeSupplierName(name)
}
