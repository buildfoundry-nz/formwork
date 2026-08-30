//go:build ignore

package local

// The single sanctioned scorer — on the carve-out; it may index the fields.
func (m *Model) RawScores(c int) float64 {
	return m.Bias[c] + dot(m.Weights[c], m.features)
}
