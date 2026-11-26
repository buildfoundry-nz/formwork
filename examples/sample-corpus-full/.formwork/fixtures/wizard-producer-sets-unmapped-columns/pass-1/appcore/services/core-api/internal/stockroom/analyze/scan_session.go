//go:build ignore

package analyze

func Analyze(cols []Column) IngestSessionPayload {
	return IngestSessionPayload{
		UnresolvedColumns: SummarizeUnmappedColumns(cols),
	}
}
