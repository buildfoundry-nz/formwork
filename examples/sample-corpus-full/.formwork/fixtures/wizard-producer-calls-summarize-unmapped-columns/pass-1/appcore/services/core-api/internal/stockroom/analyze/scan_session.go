//go:build ignore

package analyze

func Analyze(cols []Column) IngestSessionPayload {
	suppliers := ScanSuppliers(cols)
	unmapped := SummarizeUnmappedColumns(cols)
	return IngestSessionPayload{
		SupplierShortlist: suppliers,
		UnresolvedColumns: unmapped,
	}
}
