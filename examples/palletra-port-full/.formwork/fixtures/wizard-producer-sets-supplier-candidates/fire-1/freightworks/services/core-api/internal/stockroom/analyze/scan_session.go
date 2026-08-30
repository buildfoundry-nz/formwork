//go:build ignore

package analyze

// Regression: SupplierShortlist is stamped from a stale cache, not from the
// ScanSuppliers result — the call survives but the assignment dropped.
func Analyze(cols []Column) IngestSessionPayload {
	_ = ScanSuppliers(cols)
	return IngestSessionPayload{
		SupplierShortlist: storedSuppliers,
	}
}
