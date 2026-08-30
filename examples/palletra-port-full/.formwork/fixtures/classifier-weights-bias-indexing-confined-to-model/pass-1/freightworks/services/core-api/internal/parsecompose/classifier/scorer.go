//go:build ignore

package classifier

// A hand-rolled scorer would index m.Weights[c] / m.Bias[c] directly; every
// caller routes through RawScores instead.
func score(m Model, c int) float64 {
	return m.RawScores(c)
}
