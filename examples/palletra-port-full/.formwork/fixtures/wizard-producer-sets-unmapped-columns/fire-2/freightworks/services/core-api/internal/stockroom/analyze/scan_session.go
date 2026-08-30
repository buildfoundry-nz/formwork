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

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// UnresolvedColumns: SummarizeUnmappedColumns(cols),
