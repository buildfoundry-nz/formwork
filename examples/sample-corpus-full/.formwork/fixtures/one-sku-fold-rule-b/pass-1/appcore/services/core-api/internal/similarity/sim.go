//go:build ignore

package similarity

import "example.com/api/internal/pgvector"

// Delegates to the one sanctioned cosine; declares no second implementation.
func score(a, b []float64) float64 {
	return pgvector.Cosine(a, b)
}
