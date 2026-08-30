//go:build ignore

package analyze

// Regression: the ScanSuppliers call was removed, so the wizard Suppliers
// step renders empty.
func Analyze(cols []Column) IngestSessionPayload {
	unmapped := SummarizeUnmappedColumns(cols)
	return IngestSessionPayload{
		UnresolvedColumns: unmapped,
	}
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// suppliers := ScanSuppliers(cols)
