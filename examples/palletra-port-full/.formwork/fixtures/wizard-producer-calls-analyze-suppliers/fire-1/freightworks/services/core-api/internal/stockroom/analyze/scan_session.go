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
