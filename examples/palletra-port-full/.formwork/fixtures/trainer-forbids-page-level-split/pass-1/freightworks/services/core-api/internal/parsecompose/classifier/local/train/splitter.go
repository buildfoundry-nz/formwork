//go:build ignore

package train

// Document-level splitters only: DocumentSplit keeps a document's pages whole and
// DivideWithFallback degrades safely. Neither is a page-level `Split(` decl, so
// the mention of a page-level "Split" in this comment must not fire either.
func DocumentSplit(examples []Example, groups []string, frac float64) (fitSet, holdoutSet []Example) {
	return clusterPartition(examples, groups, frac)
}

func DivideWithFallback(examples []Example, minVal int) (fitSet, holdoutSet []Example, degraded bool) {
	return DocumentSplit(examples, cohortIDs(examples), 0.8), false
}
