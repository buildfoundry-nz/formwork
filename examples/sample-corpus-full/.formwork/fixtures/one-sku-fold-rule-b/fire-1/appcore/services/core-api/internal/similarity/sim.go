//go:build ignore

package similarity

// A second cosine implementation outside internal/pgvector — must not exist.
func Cosine(a, b []float64) float64 { // want: one-sku-fold-rule-b
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}
