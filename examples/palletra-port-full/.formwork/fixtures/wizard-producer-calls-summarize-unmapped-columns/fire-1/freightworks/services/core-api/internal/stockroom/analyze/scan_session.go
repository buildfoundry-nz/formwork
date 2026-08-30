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
