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

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// SupplierShortlist: ScanSuppliers(cols),
