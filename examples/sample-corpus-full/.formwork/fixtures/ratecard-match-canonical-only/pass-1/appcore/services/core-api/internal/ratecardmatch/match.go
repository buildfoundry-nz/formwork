//go:build ignore

package ratecardmatch

// Match compares canonical descriptors only (skucanon.ReferenceDescriptor).
func shortlistQuery() string {
	return `SELECT id FROM catalog WHERE canonical_label = $1`
}
