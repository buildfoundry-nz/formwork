//go:build ignore

package classifier

func score(m Model, c int) float64 {
	return m.Bias[c] // want: classifier-weights-bias-indexing-confined-to-model
}
