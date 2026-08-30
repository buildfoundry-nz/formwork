//go:build ignore

package analyze

// Regression: UnresolvedColumns is stamped from a stale cache, not from the
// SummarizeUnmappedColumns result — the call survives but the assignment dropped.
func Analyze(cols []Column) IngestSessionPayload {
	_ = SummarizeUnmappedColumns(cols)
	return IngestSessionPayload{
		UnresolvedColumns: storedColumns,
	}
}
