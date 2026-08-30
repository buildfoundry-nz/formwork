//go:build ignore

package analyze

// Regression: the SummarizeUnmappedColumns call was removed, so the wizard
// Columns step renders empty.
func Analyze(cols []Column) IngestSessionPayload {
	suppliers := ScanSuppliers(cols)
	return IngestSessionPayload{
		SupplierShortlist: suppliers,
	}
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// unmapped := SummarizeUnmappedColumns(cols)
